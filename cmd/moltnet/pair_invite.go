package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/relaydeploy"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// runPairInvite implements `moltnet pair invite`: it generates one
// shareable invite for one friend network against an already-deployed
// relay. The relay's URL and token come from --relay-url/--relay-token-env
// when given, and otherwise from the credentials `moltnet relay deploy`
// last saved to .moltnet/relay.json (see resolvePairInviteRelayCredentials).
//
// "invite show <pairing-id>" is dispatched here rather than added as its own
// case in runPairCommand (pair.go): it is a mode of "pair invite", not a
// sibling of it, mirroring how "invite" itself is a mode of "pair".
func runPairInvite(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "show" {
		return runPairInviteShow(args[1:])
	}

	flags := flag.NewFlagSet("moltnet pair invite", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		relayURLFlag  = flags.String("relay-url", "", "relay Worker URL, e.g. wss://moltnet-relay.acme.workers.dev (default: the relay from the last `moltnet relay deploy`)")
		relayTokenEnv = flags.String("relay-token-env", "", "environment variable containing the relay's RELAY_TOKEN (default: the token from the last `moltnet relay deploy`)")
		pairingID     = flags.String("id", "", "pairing id to use locally and embed in the invite (default: generated)")
		configPath    = flags.String("config", "", "Moltnet config path")
		networkID     = flags.String("network-id", "", "network id to select under ~/.moltnet when several exist")
		printOnly     = flags.Bool("print-only", false, "print the invite code without writing local config")
		force         = flags.Bool("force", false, "overwrite an existing pairing with the same id, generating a fresh invite")
		restart       = flags.Bool("restart", false, "restart the managed service for the resolved network after writing the pairing")
		verboseFlag   = flags.Bool("verbose", false, "print full detail: the wrote-pairing path, the --restart tip, remote-admin aside")
	)
	var sharedRooms repeatedStringFlag
	flags.Var(&sharedRooms, "room", "shared room id to declare in the invite (repeatable or comma-separated)")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("pair invite does not accept positional arguments")
	}

	path, err := resolvePairConfigPath(*configPath, *networkID)
	if err != nil {
		return err
	}

	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return err
	}
	if config.NetworkID == app.DefaultNetworkID {
		return fmt.Errorf(
			"network.id in %s is still the default %q; set a distinct network id before generating an invite (two default installs would collide)",
			path, app.DefaultNetworkID,
		)
	}

	relayURL, relayToken, err := resolvePairInviteRelayCredentials(path, config.NetworkID, *relayURLFlag, *relayTokenEnv)
	if err != nil {
		return err
	}

	id := strings.TrimSpace(*pairingID)
	if id == "" {
		id, err = defaultPairingID()
		if err != nil {
			return err
		}
	}

	room, err := app.GenerateRandomToken(128)
	if err != nil {
		return err
	}
	pairingToken, err := app.GenerateRandomToken(256)
	if err != nil {
		return err
	}

	invite := protocol.Invite{
		V:            protocol.InviteVersion,
		RelayURL:     relayURL,
		Room:         room,
		RelayToken:   relayToken,
		PairingToken: pairingToken,
		PairingID:    id,
		NetworkID:    config.NetworkID,
		NetworkName:  config.NetworkName,
		SharedRooms:  []string(sharedRooms),
		Exp:          time.Now().UTC().Add(protocol.DefaultInviteTTL),
	}
	if err := invite.Validate(); err != nil {
		return fmt.Errorf("generated invite failed validation: %w", err)
	}
	code, err := protocol.EncodeInvite(invite)
	if err != nil {
		return err
	}

	if *printOnly {
		fmt.Fprintln(stdout, code)
		return nil
	}

	pairing := app.PairingWriteback{
		ID:    id,
		Token: pairingToken,
		Relay: &app.PairingRelayWriteback{
			URL:   invite.RelayURL,
			Room:  room,
			Token: relayToken,
		},
	}
	authToken := app.AuthTokenWriteback{
		ID:     id,
		Value:  pairingToken,
		Scopes: []string{"pair"},
	}
	// The receipt is what makes this invite recoverable after an
	// interruption between the config commit below and the aftercare print
	// further down (see writePairingWithRollback's doc comment, pair.go).
	// Code is stored verbatim so a later `pair invite show` returns the
	// exact bytes printed here, never a re-derived approximation.
	receipt := app.InviteReceipt{
		PairingID: id,
		Code:      code,
		Exp:       invite.Exp,
	}

	if err := writePairingWithRollback(path, pairing, authToken, invite.SharedRooms, *force, &receipt); err != nil {
		if errors.Is(err, app.ErrPairingExists) {
			return fmt.Errorf("%w, or choose a different --id", err)
		}
		return err
	}

	return printPairInviteAftercare(ctx, config, path, code, id, pairAftercareOptions{
		restart: *restart,
		roomIDs: invite.SharedRooms,
		verbose: *verboseFlag,
		// remoteNetworkID/remoteNetworkName are unknown here: pair invite
		// never learns the friend's identity (no round trip); `pair <code>`
		// on their side prints the real, filled-in command instead.
	})
}

// defaultPairingID generates a short, readable pairing id when --id is not
// supplied.
func defaultPairingID() (string, error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generate pairing id: %w", err)
	}
	return "friend-" + hex.EncodeToString(suffix), nil
}

// resolvePairInviteRelayCredentials resolves the relay URL and token
// `pair invite` embeds in the generated invite. Each of relayURLFlag and
// relayTokenEnvFlag, if non-empty, wins outright for its half of the pair;
// whichever half is left unset falls back to the credentials stored at
// relaydeploy.CredentialsPath(configPath) by the last successful
// `moltnet relay deploy` (PLAN.md's "zero-flag path"). networkID is the
// already-resolved network id, threaded into the "run `moltnet relay
// deploy`" guidance so it names the exact command for this network rather
// than a bare one that silently targets the wrong network in a multi-network
// home.
func resolvePairInviteRelayCredentials(configPath, networkID, relayURLFlag, relayTokenEnvFlag string) (relayURL string, relayToken string, err error) {
	relayURL = strings.TrimSpace(relayURLFlag)

	if strings.TrimSpace(relayTokenEnvFlag) != "" {
		relayToken = strings.TrimSpace(os.Getenv(relayTokenEnvFlag))
		if relayToken == "" {
			return "", "", fmt.Errorf("environment variable %q named by --relay-token-env is empty or not set", relayTokenEnvFlag)
		}
	}

	if relayURL != "" && relayToken != "" {
		return relayURL, relayToken, nil
	}

	credentialsPath := relaydeploy.CredentialsPath(configPath)
	stored, ok, loadErr := relaydeploy.LoadCredentials(credentialsPath)
	if loadErr != nil {
		return "", "", loadErr
	}

	if relayURL == "" {
		if !ok || strings.TrimSpace(stored.URL) == "" {
			return "", "", fmt.Errorf(
				"pair invite requires --relay-url (or run `moltnet relay deploy --id %s` first to save one to %s)", networkID, credentialsPath,
			)
		}
		relayURL = strings.TrimSpace(stored.URL)
	}
	if relayToken == "" {
		if !ok || strings.TrimSpace(stored.Token) == "" {
			return "", "", fmt.Errorf(
				"pair invite requires --relay-token-env (or run `moltnet relay deploy --id %s` first to save a token to %s)", networkID, credentialsPath,
			)
		}
		relayToken = strings.TrimSpace(stored.Token)
	}

	return relayURL, relayToken, nil
}

// runPairInviteShow implements `moltnet pair invite show <pairing-id>`: the
// recovery path for an invite whose commit-then-print window was
// interrupted before the code ever reached the operator's terminal (see
// writePairingWithRollback's doc comment, pair.go), and the wizard's way to
// retrieve a code it deliberately never sees on stdout for any other child
// command (SETUP.md's "Exception: the invite code"). It never contacts the
// relay, never mutates the config, and is a pure read of the receipt
// WriteInviteReceipt wrote in the same call that committed the pairing.
//
// Argument shape mirrors "pair revoke <pairing-id>" (splitPairJoinArgs,
// pair_join.go, reused as-is): one positional pairing id plus flags.
func runPairInviteShow(args []string) error {
	flagArgs, pairingID, err := splitPairJoinArgs(args)
	if err != nil {
		return fmt.Errorf("pair invite show accepts exactly one pairing id: %w", err)
	}

	flags := flag.NewFlagSet("moltnet pair invite show", flag.ContinueOnError)
	flags.SetOutput(stdout)
	// --id means two different things one word apart: `pair invite --id X`
	// (runPairInvite, above) is the *pairing* id, but `pair invite show X
	// --id Y` is the *network* id below -- unavoidable, since the pairing id
	// is positional here, not a flag. `pair invite` itself already accepts
	// --network-id as a spelled-out alias for the network-id meaning, for
	// exactly this reason; --id is registered here too, both bound to the
	// same variable, so `show` accepts the same alias (whichever of the two
	// is given last on the command line wins, the usual alias contract).
	var (
		configPath = flags.String("config", "", "Moltnet config path")
		networkID  string
	)
	flags.StringVar(&networkID, "id", "", "network id to select under ~/.moltnet when several exist")
	flags.StringVar(&networkID, "network-id", "", "alias for --id (this command's own --id is a network id, not a pairing id, unlike `pair invite`'s)")
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	// Trim at this CLI boundary only, matching --id's own trim above in
	// runPairInvite: InviteReceiptPath hashes pairingID raw (Fix 4), so an
	// operator's typo'd surrounding whitespace would otherwise fail to find
	// a receipt that is really there, without granting an attacker any way
	// to collide two distinct ids into the same file the way trimming
	// inside the hash itself used to.
	pairingID = strings.TrimSpace(pairingID)
	if pairingID == "" {
		return fmt.Errorf("pair invite show requires a pairing id")
	}

	path, err := resolvePairConfigPath(*configPath, networkID)
	if err != nil {
		return err
	}

	receipt, ok, err := app.LoadInviteReceipt(path, pairingID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf(
			"no recoverable invite for pairing %q: it was either generated with --print-only (never written to disk), generated before this command existed, or already removed by `pair revoke %s`",
			pairingID, pairingID,
		)
	}

	// F2 (live-confirmed, two independent repros): a receipt on disk is not
	// proof the pairing it names is still live. A revoke whose config commit
	// succeeded but whose RemoveInviteReceipt step failed (e.g. a read-only
	// invites directory) leaves this exact state -- zero pairings[] entries,
	// yet a receipt handing back the full code, relay URL, and relay token
	// with exit 0. So does an interruption between the receipt commit
	// (pair.go's WriteInviteReceipt) and the pairing commit (WritePairing)
	// that follows it: the pairing never lands in config at all. Refusing
	// here whenever pairingID has no matching pairings[] entry closes both,
	// and pair revoke's own ErrPairingNotFound path (pair_revoke.go) now
	// clears exactly this orphan, so the hint below is actually true.
	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return err
	}
	if !pairingConfigured(config, pairingID) {
		return fmt.Errorf(
			"pairing %q has no entry in %s; this invite receipt is a leftover from a revoke that could not remove it, or an invite that was interrupted before ever committing -- run `moltnet pair revoke %s` to clear it, or `moltnet pair invite --id %s --force` to make it live again",
			pairingID, path, pairingID, pairingID,
		)
	}

	if receipt.Expired(time.Now()) {
		return fmt.Errorf(
			"the invite for pairing %q expired at %s; generate a new one with `moltnet pair invite --id %s --force`",
			pairingID, receipt.Exp.Format(time.RFC3339), pairingID,
		)
	}

	fmt.Fprintln(stdout, receipt.Code)
	return nil
}

// pairingConfigured reports whether config's pairings[] has an entry whose
// id exactly matches pairingID -- runPairInviteShow's guard (see its own
// F2 doc comment) against handing back a code for a pairing that is not, or
// is no longer, actually live.
func pairingConfigured(config app.Config, pairingID string) bool {
	for _, pairing := range config.Pairings {
		if pairing.ID == pairingID {
			return true
		}
	}
	return false
}
