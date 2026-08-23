package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// runRoomCreate implements `moltnet room create <id>` (PLAN.md 7A.1). The
// execution model is the point of this command, not an implementation
// detail: config is the source of truth, applied to the live service.
//
//  1. Preflight GET /v1/rooms/{id} against the running service, before
//     touching config at all, and refuse outright if it already answers
//     (refuseIfRoomAlreadyLive) — P2-1: a room created via POST /v1/rooms,
//     the console, or a separate `moltnet apply` has no entry in *this*
//     config's own rooms[], so WriteRoom's duplicate check
//     (config_writeback_room.go, ErrRoomConfigExists) never sees it. Left
//     unchecked, step 3's /v1/apply delta reaches ReconcileRoomContext
//     (internal/rooms/reconcile.go), which replaces that room's member list
//     wholesale — silently wiping a live room's membership while this
//     command still prints success. Refusing before any write leaves
//     nothing to roll back if the service already has the room.
//  2. Write the room into rooms[] in the resolved Moltnet server config,
//     using the same atomic, 0600, symlink-refusing, reload-verified
//     mechanism `pair invite` uses for pairings (writeRoomWithRollback below,
//     app.WriteRoom) — never upsertSharedRoom, which only knows the
//     {id, federation} shape a pairing needs.
//  3. Default federation to "none" unless --federation says otherwise
//     (parseRoomFederationFlag): PLAN.md's 7A federation rule requires every
//     config-time room to declare an explicit stance, or the *next*
//     `pair invite` fails to reload and rolls itself back
//     (validateRoomFederationStances, config_file.go). A --federation naming
//     pairing ids is checked against the resolved config's own pairings[]
//     (validateRoomFederationPairings) before anything is written, so a
//     typo'd pairing id is a clear error instead of a silently accepted
//     federation stance that names nothing (P2-3).
//  4. POST a single-room delta to /v1/apply (admin-scoped) so the running
//     service reconciles the room — and its credential, which only ever
//     lands in memory through CreateRoomContext/ReconcileRoomContext's
//     setRoomCredential call — without a restart. This is deliberately never
//     a full-config apply: ReconcileRoomContext replaces a named room's
//     member list wholesale, so re-applying every configured room while
//     creating this one would silently wipe membership `admin room members
//     add` granted any other room since its last config writeback.
//  5. Fall back to a restart reminder, never an error, when the running
//     service is not answering: the config write already stands durably
//     either way.
func runRoomCreate(ctx context.Context, args []string) error {
	flagArgs, roomIDArg, err := splitSingleRoomIDArgs(args, roomCreateValueFlags, "room create")
	if err != nil {
		return err
	}

	flags := flag.NewFlagSet("moltnet room create", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		name          = flags.String("name", "", "room display name (default: the room id)")
		visibility    = flags.String("visibility", "", "room visibility: private or public (default: private)")
		writePolicy   = flags.String("write-policy", "", "room write policy: members, registered_agents, or operators (default: members)")
		federationRaw = flags.String("federation", "", `room federation stance: "none", "all", or a comma-separated list of pairing ids (default: none)`)
		credential    = flags.String("credential", "", "plaintext credential agents must present to `room join` this room (default: none, an unjoinable room)")
		networkID     = flags.String("network", "", "Moltnet network id to select under ~/.moltnet when several exist")
		baseURLFlag   = flags.String("base-url", "", "explicit base URL for the running Moltnet service (default: derived from the resolved config's server.listen_addr)")
		tokenEnvFlag  = flags.String("token-env", "", "environment variable holding the bearer token for --base-url")
	)
	var members repeatedStringFlag
	flags.Var(&members, "member", "member id to seed the room with; may be repeated or comma-separated")

	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("room create does not accept extra positional arguments")
	}
	roomID := strings.TrimSpace(roomIDArg)
	if roomID == "" {
		return fmt.Errorf("room create requires exactly one room id")
	}
	if err := protocol.ValidateRoomID(roomID); err != nil {
		return fmt.Errorf("room id %w", err)
	}
	if err := protocol.ValidateRoomVisibility(*visibility); err != nil {
		return err
	}
	if err := protocol.ValidateRoomWritePolicy(*writePolicy); err != nil {
		return err
	}
	federation, err := parseRoomFederationFlag(*federationRaw)
	if err != nil {
		return err
	}
	memberIDs := protocol.UniqueTrimmedStrings(members)
	for _, memberID := range memberIDs {
		if err := protocol.ValidateMemberID(memberID); err != nil {
			return fmt.Errorf("--member %q is invalid: %w", memberID, err)
		}
	}

	path, found, err := app.ResolveConfigPath("", strings.TrimSpace(*networkID))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("moltnet config not found; run `moltnet init` first")
	}

	// Loaded once, before any write: WriteRoom only ever appends to rooms[]
	// (config_writeback_room.go), so this pre-write config's own
	// server.listen_addr/auth.tokens/pairings[] are exactly what the
	// post-write reload inside writeRoomWithRollback would also report —
	// reusing it here for the live-room preflight and the federation
	// pairing check needs no second reload, and lets both refuse before a
	// single byte of config changes.
	preConfig, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return fmt.Errorf("load Moltnet config %q: %w", path, err)
	}

	if err := validateRoomFederationPairings(federation, preConfig.Pairings); err != nil {
		return err
	}

	attachment := resolveRoomApplyAttachment(preConfig, *baseURLFlag, *tokenEnvFlag)
	serviceReachable := probeConsoleHealth(ctx, attachment.BaseURL)
	if serviceReachable {
		if err := refuseIfRoomAlreadyLive(ctx, attachment, roomID); err != nil {
			return err
		}
	}

	writeback := app.RoomWriteback{
		ID:          roomID,
		Name:        strings.TrimSpace(*name),
		Visibility:  strings.TrimSpace(*visibility),
		WritePolicy: strings.TrimSpace(*writePolicy),
		Members:     memberIDs,
		Federation:  federation,
		Credential:  strings.TrimSpace(*credential),
	}

	config, err := writeRoomWithRollback(path, writeback)
	if err != nil {
		return err
	}

	applyRequest := protocol.ApplyConfigRequest{
		Rooms: []protocol.CreateRoomRequest{
			{
				ID:          roomID,
				Name:        writeback.Name,
				Members:     writeback.Members,
				Visibility:  writeback.Visibility,
				WritePolicy: writeback.WritePolicy,
				Federation:  federation,
				Credential:  protocol.NewSecretString(writeback.Credential),
			},
		},
	}

	if !serviceReachable {
		printRoomCreateRestartReminder(config.NetworkID, roomID)
		return nil
	}

	client, err := moltnetclient.New(attachment)
	if err != nil {
		return err
	}
	result, err := client.ApplyConfig(commandContext(), applyRequest)
	if err != nil {
		return fmt.Errorf("wrote room %q to config but the running service rejected it: %w", roomID, err)
	}
	if len(result.Rooms) == 1 {
		return printJSON(result.Rooms[0])
	}
	return printJSON(result)
}

// refuseIfRoomAlreadyLive is P2-1's guard: it calls GET /v1/rooms/{roomID}
// against the already-confirmed-reachable service and refuses outright if
// it answers, since `room create` never updates an existing room (see
// runRoomCreate's doc comment, step 1). A GetRoom error here is not treated
// as "room absent" — it is almost always the service's real 404 for an id
// that does not exist yet (internal/rooms/errors.go, store.ErrRoomNotFound),
// but this function does not need to disambiguate that from a different
// live error: either way, "the service did not affirmatively confirm the
// room exists" is exactly runRoomCreate's original behavior before this
// guard existed, and any other real problem (an admin token the service
// rejects, for instance) surfaces again, identically, at the /v1/apply call
// runRoomCreate makes right after — this guard only ever adds a new refusal
// path, never removes an existing error surface.
func refuseIfRoomAlreadyLive(ctx context.Context, attachment clientconfig.AttachmentConfig, roomID string) error {
	client, err := moltnetclient.New(attachment)
	if err != nil {
		return nil
	}
	if _, err := client.GetRoom(ctx, roomID); err == nil {
		return fmt.Errorf(
			"room %q already exists on the running service; \"moltnet room create\" never updates an existing room, since doing so over /v1/apply would replace its member list wholesale (ReconcileRoomContext) — use \"moltnet admin room members add\" to change membership, or pick a different room id",
			roomID,
		)
	}
	return nil
}

// writeRoomWithRollback writes room into the Moltnet config file at path
// (app.WriteRoom — the atomic, reload-verified mechanism WritePairing uses
// for pairings), then reloads the full config as a post-write check,
// restoring the file to its prior contents if the reload fails. It returns
// the reloaded config on success so the caller has the room now actually
// present in config.Rooms (runRoomCreate only needs config.NetworkID off of
// it today, for the restart reminder, but returning the real post-write
// config rather than the pre-write one it already had stays correct if that
// ever changes).
func writeRoomWithRollback(path string, room app.RoomWriteback) (app.Config, error) {
	snapshot, err := snapshotFile(path)
	if err != nil {
		return app.Config{}, err
	}

	if err := app.WriteRoom(path, room); err != nil {
		return app.Config{}, err
	}

	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		if restoreErr := snapshot.restore(path); restoreErr != nil {
			return app.Config{}, fmt.Errorf("wrote room but the config failed to reload (%v); restore also failed: %w", err, restoreErr)
		}
		return app.Config{}, fmt.Errorf("wrote room but the config failed to reload; rolled back: %w", err)
	}

	return config, nil
}

// resolveRoomApplyAttachment resolves where `room create` sends its
// single-room /v1/apply delta: an explicit --base-url always wins outright
// (operatorOverrideOptions' own semantics, client_operator_fallback.go),
// otherwise it is derived from the same local config `room create` resolved
// (the pre-write load runRoomCreate already did for the P2-1 live-room
// preflight — WriteRoom never touches server.listen_addr/auth.tokens, so a
// pre- or post-write config reports the same attachment either way), exactly
// like admin's zero-flag fallback (resolveAdminFromServerConfig,
// admin_config_fallback.go) — an operator creating a room on the network
// they are standing next to never has to pass --base-url/--token-env at all.
func resolveRoomApplyAttachment(config app.Config, baseURLFlag, tokenEnvFlag string) clientconfig.AttachmentConfig {
	if strings.TrimSpace(baseURLFlag) != "" {
		override := operatorOverrideOptions{baseURL: baseURLFlag, tokenEnv: tokenEnvFlag}
		return override.attachment()
	}
	return clientconfig.AttachmentConfig{
		BaseURL:   localBaseURLHint(config),
		NetworkID: config.NetworkID,
		Auth:      adminAuthFromServerConfig(config),
	}
}

// printRoomCreateRestartReminder is `room create`'s fallback for a running
// service that is not answering (PLAN.md 7A.1's "fall back to the existing
// restart offer only when the daemon is not answering"). The room is
// already durably written to config either way, so this is a reminder, not
// an error — unlike a failed /v1/apply call, where the running service
// answered but rejected the delta, which is a real problem the operator
// needs to see. There is no --restart flag here to act on directly (unlike
// `pair invite`): the first time this fires, the service has often never
// been started at all.
// It writes to stderr, not stdout: stdout is the success path's JSON
// (printJSON(result.Rooms[0])), and a caller piping `moltnet room create x |
// jq` on the fallback path must see clean JSON on stdout with nothing else
// mixed in, exactly like remove.go's deprecation note already does.
//
// It also carries the P2 interim mitigation for the unreachable path's real
// gap: refuseIfRoomAlreadyLive (runRoomCreate, step 1) never runs when
// serviceReachable is false, so this command cannot tell whether roomID
// already exists in the persistent store under a different config's rooms[]
// (the same P2-1 scenario TestRoomCreateRefusesLiveRoomNotTrackedInConfig
// covers for the reachable path). If it does, the next service start runs
// applyRequestFromConfig (internal/app/app.go) down into
// ReconcileRoomContext (internal/rooms/reconcile.go), which replaces that
// room's member list wholesale from exactly the config entry this command
// just wrote — dropping any member the store already had that is not listed
// here. This is a warning, not a refusal: the real fix is Phase 7B's `admin
// room members add` config writeback, and refusing outright here would
// block the common case (a genuinely new room id) on nothing more than "the
// daemon happens to be down right now."
func printRoomCreateRestartReminder(networkID, roomID string) {
	fmt.Fprintf(stderr, "  %s the Moltnet server for %s is not answering\n", yellow("warning:"), networkID)
	fmt.Fprintf(stderr, "  %s liveness could not be verified, so it is unknown whether room %q already exists in the store; if it does, its membership will be reconciled from this config entry at next start, dropping any member already there that is not listed here\n", yellow("warning:"), roomID)
	fmt.Fprintf(stderr, "  start it (\"moltnet service start\" or \"moltnet start\") and re-run so the preflight can check, or list the intended members now with --member\n")
}

// parseRoomFederationFlag parses --federation into a *protocol.RoomFederation:
// "none" or "all" map to their scalar stances, anything else is treated as a
// comma-separated list of pairing ids (protocol.RoomFederationList), and an
// empty value defaults to protocol.DefaultRoomFederation() ("none") — never
// nil, per PLAN.md's 7A federation rule (see runRoomCreate's doc comment).
//
// The "none"/"all" comparison is case-folded (P2-3): protocol.RoomFederation
// stores and compares its Mode as an exact-case constant everywhere else
// (protocol.RoomFederationAllows's switch, MarshalJSON), so an operator
// typing "All" or "NONE" must land on the canonical lowercase mode here,
// not silently fall through to the comma-separated-list branch below —
// which is exactly what used to happen: "All" or "NONE" parsed as a
// one-pairing federation list naming a pairing literally spelled "All" or
// "NONE", accepted with err=<nil>. This function only proves the *shape* of
// a list entry is a valid id (protocol.ValidateMessageID, inside
// protocol.ValidateRoomFederation); whether it names a pairing this network
// actually has configured is validateRoomFederationPairings's job, run
// separately once the config is loaded (runRoomCreate), since this
// function has no config to check against.
func parseRoomFederationFlag(value string) (*protocol.RoomFederation, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return protocol.DefaultRoomFederation(), nil
	}

	var federation *protocol.RoomFederation
	switch {
	case strings.EqualFold(trimmed, protocol.RoomFederationNone):
		federation = &protocol.RoomFederation{Mode: protocol.RoomFederationNone}
	case strings.EqualFold(trimmed, protocol.RoomFederationAll):
		federation = &protocol.RoomFederation{Mode: protocol.RoomFederationAll}
	default:
		federation = &protocol.RoomFederation{
			Mode:     protocol.RoomFederationList,
			Pairings: protocol.UniqueTrimmedStrings(strings.Split(trimmed, ",")),
		}
	}
	if err := protocol.ValidateRoomFederation(federation); err != nil {
		return nil, fmt.Errorf("--federation %q is invalid: %w", value, err)
	}
	return federation, nil
}

// validateRoomFederationPairings is P2-3's other half: ValidateRoomFederation
// (pkg/protocol/federation.go) only checks that each --federation list entry
// has the *shape* of an id (protocol.ValidateMessageID) — it has no config to
// check membership against, since protocol stays transport- and
// config-neutral. A plain typo like "nonw" (not close enough to "none"/"all"
// for parseRoomFederationFlag's case-fold to catch) shape-validates cleanly
// and used to become a one-pairing federation list naming a pairing that
// does not exist, accepted with err=<nil>: the room reports success while
// federating to nothing. This runs once the resolved config is loaded
// (runRoomCreate, before the write), checking every list entry against
// config.Pairings and naming the configured ids in the error so the
// operator can fix the typo without a second round trip.
func validateRoomFederationPairings(federation *protocol.RoomFederation, pairings []protocol.Pairing) error {
	if federation == nil || federation.Mode != protocol.RoomFederationList {
		return nil
	}

	validIDs := make([]string, 0, len(pairings))
	known := make(map[string]bool, len(pairings))
	for _, pairing := range pairings {
		known[pairing.ID] = true
		validIDs = append(validIDs, pairing.ID)
	}
	sort.Strings(validIDs)

	var unknown []string
	for _, pairingID := range federation.Pairings {
		if !known[pairingID] {
			unknown = append(unknown, pairingID)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	if len(validIDs) == 0 {
		return fmt.Errorf("--federation names unknown pairing id(s) %s: this network has no configured pairings", strings.Join(unknown, ", "))
	}
	return fmt.Errorf("--federation names unknown pairing id(s) %s: configured pairing ids are %s", strings.Join(unknown, ", "), strings.Join(validIDs, ", "))
}
