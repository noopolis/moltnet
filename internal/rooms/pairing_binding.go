package rooms

import (
	"context"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/observability"
)

// pairCredentialMatchesOrigin decides whether a pair-scoped credential may
// assert originNetworkID as the origin of an inbound relayed message.
//
// Three cases, in order:
//
//  1. The credential carries a network binding from config
//     (bindPairingNetworks, internal/app/app.go) -- the peer's network id was
//     known at pairing time. This is the join side: `moltnet pair <code>`
//     learns the inviter's network id straight out of the invite. Compare and
//     be done.
//
//  2. No config binding, but this credential has already been seen asserting
//     an origin during this process's lifetime -- compare against that learned
//     value. A second, different origin from the same credential is a
//     cross-pairing impersonation attempt and is refused.
//
//  3. No config binding and never seen before -- first contact. Under strict
//     mode refuse (unchanged). Otherwise learn it and accept, so case 2 can
//     enforce consistency from here on.
//
// Case 2/3 exist because the INVITING side never learns its peer's real
// network id: `pair invite` mints the credential before the peer has ever
// spoken, so `bindPairingNetworks` has nothing to bind from and
// claims.Network() stays empty indefinitely (see
// reference/authentication.md#require_pair_network_binding). Before this,
// that meant an unbound credential could assert *any* origin network id on
// every message forever -- the hole the old code logged about but allowed.
// Trust-on-first-use closes it: whatever the peer claims first is what it is
// held to.
//
// This binding is deliberately process-local, alongside pairingStatuses
// (service.go), and is NOT written back to config: the server has never
// written its own config, and introducing a second writer racing the
// `pair invite`/`pair revoke` CLI commands over one file is a worse trade
// than re-learning on restart. The residual window is one message per
// credential per process start, and only for credentials config never bound.
func (s *Service) pairCredentialMatchesOrigin(ctx context.Context, claims authn.Claims, originNetworkID string, strict bool) bool {
	if credentialNetworkID := strings.TrimSpace(claims.Network()); credentialNetworkID != "" {
		return credentialNetworkID == originNetworkID
	}

	credentialKey := pairBindingKey(claims)
	if credentialKey == "" {
		// No stable way to identify this credential across messages, so
		// there is nothing to pin a learned binding to. Fall back to the
		// pre-existing behavior rather than inventing a key.
		if strict {
			logPairBindingRefusal(ctx, originNetworkID)
			return false
		}
		return true
	}

	if strict {
		// Strict mode never learns: only a binding config established at
		// pairing time counts, and this credential has none.
		logPairBindingRefusal(ctx, originNetworkID)
		return false
	}

	matched, firstContact := s.bindOrMatchPairNetwork(credentialKey, originNetworkID)
	switch {
	case firstContact:
		observability.Logger(ctx, "rooms.pairing", "origin_network_id", originNetworkID).
			Info("binding this pair credential to the origin network it first asserted; later messages claiming a different origin will be rejected")
	case !matched:
		observability.Logger(ctx, "rooms.pairing", "origin_network_id", originNetworkID).
			Warn("rejecting inbound pair-scoped message whose origin network id differs from the one this credential first asserted")
	}
	return matched
}

// bindOrMatchPairNetwork compares originNetworkID against this credential's
// learned binding, establishing it if there is none — as ONE atomic step
// under a single exclusive lock.
//
// Splitting this into a read followed by a later write loses the guarantee
// entirely: two concurrent first-contact messages asserting different origins
// would both find no binding, both proceed, and both be accepted, with only
// later messages constrained to whichever write happened to land. The whole
// point is that the first origin wins and every other origin loses, so the
// comparison and the establishment cannot be separated.
//
// Returns whether the origin is acceptable, and whether this call is the one
// that established the binding.
func (s *Service) bindOrMatchPairNetwork(credentialKey, originNetworkID string) (matched bool, firstContact bool) {
	s.pairingsMu.Lock()
	defer s.pairingsMu.Unlock()

	if s.learnedPairNetworks == nil {
		s.learnedPairNetworks = map[string]string{}
	}
	if learned, ok := s.learnedPairNetworks[credentialKey]; ok {
		return learned == originNetworkID, false
	}
	s.learnedPairNetworks[credentialKey] = originNetworkID
	return true, true
}

func logPairBindingRefusal(ctx context.Context, originNetworkID string) {
	observability.Logger(ctx, "rooms.pairing", "origin_network_id", originNetworkID).
		Warn("rejecting inbound pair-scoped message from a pair token with no bound network because auth.require_pair_network_binding is enabled")
}

// pairBindingKey identifies the credential a learned binding is pinned to.
// TokenID is the auth.tokens[] id, which pair invite/join mint equal to the
// pairing id (cmd/moltnet/pair_invite.go), so a revoked-and-reminted pairing
// under the same id produces the same key -- acceptable here because revoking
// deletes the token, and a fresh credential legitimately re-learns on its own
// first contact after the restart that reload requires. CredentialKey is the
// fallback for shapes that carry no token id.
func pairBindingKey(claims authn.Claims) string {
	if tokenID := strings.TrimSpace(claims.TokenID); tokenID != "" {
		return "token:" + tokenID
	}
	return strings.TrimSpace(claims.CredentialKey)
}
