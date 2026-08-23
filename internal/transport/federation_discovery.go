package transport

import (
	"context"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// filterPairScopedRoomDiscovery is a serving-side guard for FetchRooms. The
// room service applies the same filter before pagination; keeping this check at
// the HTTP boundary prevents another Service implementation from exposing a
// room to a peer whose bound pairing is not federated with it.
//
// F1 (review round 2, confirmed live): this is a second federation gate on
// GET /v1/rooms, applied after ListRoomsContext's own filter
// (internal/rooms/federation_access.go). Round 1 added an operator bypass
// only to that first gate, so a four-scope [observe, write, admin, pair]
// operator token -- the exact shape `--bearer` init used to mint, per
// isOperatorClaims's doc comment in federation_access.go -- passed the first
// gate untouched only to be nulled out here: this function keyed on
// claims.Allows(ScopePair) with no operator bypass at all, and an operator
// credential carries no real pairing (nothing named "operator" in
// pairings[]), so remoteNetworkID resolves empty and the whole page comes
// back nil. The isOperatorClaims check below mirrors that first gate (and
// access_policy.go's canWriteRoom) exactly, so "operator" means the same
// thing in every gate that inspects the pair scope.
//
// P1-1 (final-gate review, confirmed live): this function used to resolve
// the caller's pairing itself, by claims.Network() alone -- a second,
// independent pairing-resolution strategy that diverged from
// internal/rooms/federation_access.go's own gate, which resolves by
// credential identity (claims.TokenID) first and only falls back to the
// network binding. `pair invite` never persists a network binding on the
// inviter side (see PairingForPairScopedContext's doc comment for why), so
// every inviter-issued pairing credential resolved to nothing here and
// GET /v1/rooms came back empty for it, even on a room the rooms-package
// gate would have allowed. This now delegates to
// service.PairingForPairScopedContext -- the one exported resolution
// method rooms.Service already uses for every same-process gate -- so the
// two can no longer diverge: there is exactly one strategy, defined once.
func filterPairScopedRoomDiscovery(ctx context.Context, service Service, page protocol.RoomPage) (protocol.RoomPage, error) {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) || isOperatorClaims(claims) {
		return page, nil
	}

	pairing, ok := service.PairingForPairScopedContext(ctx)
	if !ok {
		page.Rooms = nil
		return page, nil
	}

	rooms := make([]protocol.Room, 0, len(page.Rooms))
	for _, room := range page.Rooms {
		if protocol.RoomFederationAllows(room.Federation, pairing.ID) {
			rooms = append(rooms, room)
		}
	}
	page.Rooms = rooms
	return page, nil
}

// isOperatorClaims reports whether claims describes an operator credential
// rather than a peer/pairing credential, regardless of whether it also
// carries the pair scope. This deliberately duplicates
// internal/rooms/federation_access.go's function of the same name (and
// access_policy.go's inline admin+write check) instead of importing rooms
// from transport: transport maps requests to the Service interface and does
// not otherwise depend on room package internals (see
// internal/transport/CLAUDE.md), and the two-line check is cheap enough to
// keep in sync deliberately rather than worth a cross-package dependency.
// If this definition of "operator" ever changes, update all three call
// sites together.
func isOperatorClaims(claims authn.Claims) bool {
	return claims.Allows(authn.ScopeAdmin) && claims.Allows(authn.ScopeWrite)
}
