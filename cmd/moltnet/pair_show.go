package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// pairShowResult is `moltnet pair show`'s combined JSON output: the peer's
// live network document, its rooms, and its agents, exactly the three legs
// GET /v1/pairings/{id}/network|rooms|agents expose.
type pairShowResult struct {
	Network protocol.Network        `json:"network"`
	Rooms   []protocol.Room         `json:"rooms"`
	Agents  []protocol.AgentSummary `json:"agents"`
}

// runPairShow implements `moltnet pair show <pairing-id>` (PLAN.md 7B.4): it
// replaces both sides' printed placeholder ("<their-agent-id>" in the
// membership-command aftercare, pair_aftercare.go) with the peer's real
// rooms and agents -- but only once, since PairingAgentsContext
// (internal/rooms/pairings.go) refreshes diagnostics and calls FetchAgents
// live over the pairing transport on every call. There is no cache to fall
// back to: a pending invite (the peer has never answered) or an unreachable
// peer both surface as internal/rooms.ErrRemotePairing (HTTP 502) from the
// server, and this command's job is to say so in plain language instead of
// handing back that transport error verbatim.
func runPairShow(args []string) error {
	// splitApplyArgs (remove.go) already separates "one positional token
	// plus admin-style value flags" generically -- it is not specific to
	// "moltnet apply"'s config path despite the name, and its recognized
	// flag set (--base-url/--config/--network/--token/...) is exactly
	// bindAdminClientResolverFlags's, which Go's flag package cannot parse
	// correctly on its own here since it stops at the first non-flag
	// argument and the pairing id is documented to come first.
	flagArgs, pairingID, err := splitApplyArgs(args)
	if err != nil {
		return fmt.Errorf("pair show accepts exactly one pairing id: %w", err)
	}
	if pairingID == "" {
		return fmt.Errorf("pair show requires exactly one pairing id")
	}

	flags := flag.NewFlagSet("moltnet pair show", flag.ContinueOnError)
	flags.SetOutput(stdout)
	options := bindAdminClientResolverFlags(flags, false)
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}

	client, err := resolveAdminClient(flags, options)
	if err != nil {
		return err
	}

	ctx := commandContext()
	network, err := client.GetPairingNetwork(ctx, pairingID)
	if err != nil {
		return pairShowError(ctx, client, pairingID, err)
	}
	rooms, err := client.GetPairingRooms(ctx, pairingID, protocol.PageRequest{})
	if err != nil {
		return pairShowError(ctx, client, pairingID, err)
	}
	agents, err := client.GetPairingAgents(ctx, pairingID, protocol.PageRequest{})
	if err != nil {
		return pairShowError(ctx, client, pairingID, err)
	}

	return printJSON(pairShowResult{Network: network, Rooms: rooms.Rooms, Agents: agents.Agents})
}

// pairShowError translates an unreachable-peer transport failure into a
// plain-language message naming the pairing, and passes any other error
// through unchanged -- a protocol incompatibility or discovery error already
// carries a meaningful server-supplied message (internal/rooms/pairing_diagnostics.go),
// so only the specific "could not even talk to the peer" case needs
// translating.
//
// F5 (review round 2): a revoked pairing's live legs 404 with "unknown
// pairing %q" -- findPairing (internal/rooms/pairings.go) fails closed
// identically for "never existed" and "revoked", by design, so this command
// must never make a live request over a revoked pairing's stored credential
// either way. But `pair list` shows that same pairing with
// `"status": "revoked"` in the same breath, so telling this command's
// caller it is "unknown" is actively misleading, not just imprecise. On a
// 404, check whether `pair list` still knows about this id before deciding
// which message to print.
func pairShowError(ctx context.Context, client *moltnetclient.Client, pairingID string, err error) error {
	if isPairingUnreachableError(err) {
		return fmt.Errorf("pairing %q's peer is unreachable: it may not have answered this invite yet, or its network is currently down; try again once it is reachable", pairingID)
	}
	if isPairingNotFoundError(err) && pairingIsRevoked(ctx, client, pairingID) {
		return fmt.Errorf("pairing %q has been revoked; run `moltnet pair list` to see its recorded status", pairingID)
	}
	return err
}

// isPairingNotFoundError reports whether err is the HTTP 404
// internal/rooms.unknownPairingError produces, via moltnetclient.APIError's
// typed Status field (see isPairingUnreachableError's doc comment for why
// that field, not string-matching, is what's trustworthy here).
func isPairingNotFoundError(err error) bool {
	var apiErr *moltnetclient.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// pairingIsRevoked reports whether pairingID appears in `pair list`'s own
// output with a revoked status. A failure to even list pairings (e.g. the
// credential lacks list access) is treated as "can't confirm revoked" rather
// than compounding one error into another -- the caller falls back to the
// original 404 in that case.
func pairingIsRevoked(ctx context.Context, client *moltnetclient.Client, pairingID string) bool {
	page, err := client.ListPairings(ctx, protocol.PageRequest{})
	if err != nil {
		return false
	}
	for _, pairing := range page.Pairings {
		if pairing.ID == pairingID {
			return strings.EqualFold(strings.TrimSpace(pairing.Status), protocol.PairingStatusRevoked)
		}
	}
	return false
}

// isPairingUnreachableError reports whether err is the HTTP 502
// internal/rooms.remotePairingError produces when a live pairing request
// (FetchNetwork/FetchRooms/FetchAgents) fails outright, or when the peer is
// reachable but discovery-incompatible (pairingDiscoveryError, wrapped
// through the same remotePairingError) -- both are conditions this command
// cannot do anything about except say "not right now", so both get the same
// friendly message. The specific internal message (e.g. the underlying dial
// error) never reaches the client: internal/transport/helpers.go's
// writeError deliberately writes only a generic {"code":"bad_gateway",
// "error":"bad gateway"} body for a 5xx, logging the detailed message
// server-side instead.
//
// This used to substring-match the literal text "bad_gateway" against the
// whole formatted error, which also embeds the request URL and body -- both
// containing the caller-supplied pairing id. `moltnet pair show bad_gateway`
// against an *unknown* pairing (a real 404) matched that substring purely
// because the id itself contained it, and reported "peer is unreachable"
// for what was actually a not-found error. moltnetclient.APIError's typed
// Status field is checked instead, which cannot be spoofed by the pairing
// id or any other request content.
func isPairingUnreachableError(err error) bool {
	var apiErr *moltnetclient.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusBadGateway
}
