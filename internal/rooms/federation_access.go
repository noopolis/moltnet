package rooms

import (
	"context"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// pairingForPairScopedContext resolves the pairing behind a pair-scoped
// credential.
//
// 7B.1 placeholder binding: `pair invite` and `pair <code>` always mint the
// pair-scoped auth.tokens[] entry with the same id as the pairings[] entry
// it belongs to (see PairingWriteback/AuthTokenWriteback constructions in
// cmd/moltnet/pair_invite.go and cmd/moltnet/pair_join.go, unchanged by this
// fix) — so claims.TokenID already identifies the exact pairing a bearer
// secret belongs to, independent of claims.Network(). That identity binding
// is the "placeholder": it exists the instant a credential is minted and
// requires no round trip to the peer, whereas the network binding
// (claims.Network(), filled in by bindPairingNetworks once the peer's real
// remote_network_id is known) is only "confirmed" later, at first contact.
// Resolving by TokenID first means federation enforcement below no longer
// depends on that confirmation ever happening: an inviter's freshly-minted,
// not-yet-contacted peer credential is enforced against the room's
// federation list from the moment it is written, not left to bypass it.
//
// Every pairing ever written by this codebase already satisfies the
// TokenID-equals-pairing-ID invariant (it predates this change), so no
// config migration is needed. The only credential this newly refuses is one
// an operator hand-edited into auth.tokens[] with the `pair` scope, no bound
// network, and an id that does not match any configured pairing — which is
// exactly the shape of credential this fix exists to stop trusting
// unconditionally; failing it closed is the intended behavior, not a
// regression.
//
// The claims.Network() lookup remains as a fallback for credentials that do
// carry a confirmed network binding (join-side tokens, which know the
// inviter's network id immediately from the invite, and inviter-side tokens
// once a later mechanism confirms and persists the peer's real network id).
func (s *Service) pairingForPairScopedContext(ctx context.Context) (protocol.Pairing, bool) {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) {
		return protocol.Pairing{}, false
	}
	if pairing, err := s.findPairingByID(claims.TokenID); err == nil {
		return pairing, true
	}
	remoteNetworkID := strings.TrimSpace(claims.Network())
	if remoteNetworkID == "" {
		return protocol.Pairing{}, false
	}
	pairing, err := s.findPairingByRemoteNetwork(remoteNetworkID)
	return pairing, err == nil
}

// PairingForPairScopedContext is pairingForPairScopedContext, exported so
// internal/transport's federation discovery gate (filterPairScopedRoomDiscovery,
// federation_discovery.go) can share the exact same resolution strategy
// instead of re-deriving its own.
//
// P1-1 (final-gate review, confirmed live): before this export existed,
// that transport-layer gate matched a pair-scoped caller's pairing by
// claims.Network() alone -- the network-fallback half of this method's own
// strategy, with no token-id-first lookup at all. `pair invite` never
// persists a network binding on the inviter side (that is why the 7B.2
// default flip was reverted, see PLAN.md 7B.2), so every inviter-issued
// pairing credential resolved to nothing and GET /v1/rooms came back
// empty for it, even though this same service's own room-visibility gate
// (roomVisibleToPairScopedContext, right below) would have allowed its
// federated rooms via the token-id lookup above. The two gates enforced
// different pairing identities for the same request. Exporting this one
// resolution method -- rather than exporting a second predicate transport
// re-implements independently, the mistake that produced the divergence in
// the first place -- means there is exactly one strategy for "which
// pairing does this pair-scoped credential belong to," and every gate that
// needs it calls this.
func (s *Service) PairingForPairScopedContext(ctx context.Context) (protocol.Pairing, bool) {
	return s.pairingForPairScopedContext(ctx)
}

// pairScopedWriteAllowed and roomVisibleToPairScopedContext used to return
// true outright whenever a pair-scoped claim carried no bound network —
// the inviter-side fail-open 7B.1 closes: `pair invite` mints exactly that
// shape of credential (see pairingForPairScopedContext's doc comment), so
// every room's federation list was advisory on the inviting side. Both
// gates now always resolve the caller's actual pairing (by credential
// identity, falling back to network binding) and deny when that fails,
// instead of special-casing an empty network as "not a peer".
//
// F1 (confirmed live): a claim without the `pair` scope at all is not the
// only shape that must bypass the pairing lookup below. `--bearer` init
// used to mint (and existing configs in the wild still have) an operator
// token scoped [observe, write, admin, pair] — carrying `pair` alongside
// `admin`/`write` was meant to let the same token double as a pairing
// credential, but it meant this gate treated that operator as a peer,
// looked for a pairing literally named "operator", found none, and denied
// every write and hid every room from its own owner. isOperatorClaims below
// names that shape explicitly (mirroring canWriteRoom's own admin+write
// check in access_policy.go, so "operator" means the same thing in every
// gate) and both functions bypass the pairing lookup for it, exactly like
// they already do for a claim with no `pair` scope at all. Fresh `--bearer`
// tokens are minted without `pair` now (cmd/moltnet/templates.go,
// cmd/moltnet/init_server_config.go), but this bypass is what makes the fix
// safe for every four-scope token already deployed, not just new ones.
func (s *Service) pairScopedWriteAllowed(ctx context.Context, room protocol.Room) bool {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) || isOperatorClaims(claims) {
		return true
	}
	pairing, ok := s.pairingForPairScopedContext(ctx)
	return ok && protocol.RoomFederationAllows(room.Federation, pairing.ID)
}

func (s *Service) roomVisibleToPairScopedContext(ctx context.Context, room protocol.Room) bool {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) || isOperatorClaims(claims) {
		return true
	}
	pairing, ok := s.pairingForPairScopedContext(ctx)
	return ok && protocol.RoomFederationAllows(room.Federation, pairing.ID)
}

// isOperatorClaims reports whether claims describes an operator credential
// rather than a peer/pairing credential, regardless of whether it also
// carries the `pair` scope. This mirrors canWriteRoom's existing admin+write
// check (access_policy.go) so "operator" is defined the same way in every
// gate, instead of by scope-counting or by the mere absence of `pair` (which
// broke, per F1, the moment an operator token also carried `pair`).
func isOperatorClaims(claims authn.Claims) bool {
	return claims.Allows(authn.ScopeAdmin) && claims.Allows(authn.ScopeWrite)
}

func (s *Service) filterPairScopedAgentSummaries(ctx context.Context, agents []protocol.AgentSummary) []protocol.AgentSummary {
	if _, scoped := s.pairingForPairScopedContext(ctx); !scoped {
		return agents
	}
	filtered := make([]protocol.AgentSummary, 0, len(agents))
	for _, agent := range agents {
		rooms := make([]string, 0, len(agent.Rooms))
		for _, roomID := range agent.Rooms {
			room, ok, err := s.getRoom(ctx, roomID)
			if err == nil && ok && s.roomVisibleToPairScopedContext(ctx, room) {
				rooms = append(rooms, roomID)
			}
		}
		if len(rooms) == 0 {
			continue
		}
		agent.Rooms = rooms
		filtered = append(filtered, agent)
	}
	return filtered
}
