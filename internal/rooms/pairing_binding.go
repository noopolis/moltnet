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

	if learned, ok := s.learnedPairNetwork(credentialKey); ok {
		if learned == originNetworkID {
			return true
		}
		observability.Logger(ctx, "rooms.pairing",
			"origin_network_id", originNetworkID, "bound_network_id", learned).
			Warn("rejecting inbound pair-scoped message whose origin network id differs from the one this credential first asserted")
		return false
	}

	if strict {
		logPairBindingRefusal(ctx, originNetworkID)
		return false
	}

	s.learnPairNetwork(credentialKey, originNetworkID)
	observability.Logger(ctx, "rooms.pairing", "origin_network_id", originNetworkID).
		Info("binding this pair credential to the origin network it first asserted; later messages claiming a different origin will be rejected")
	return true
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

func (s *Service) learnedPairNetwork(credentialKey string) (string, bool) {
	s.pairingsMu.RLock()
	defer s.pairingsMu.RUnlock()
	networkID, ok := s.learnedPairNetworks[credentialKey]
	return networkID, ok
}

func (s *Service) learnPairNetwork(credentialKey, networkID string) {
	s.pairingsMu.Lock()
	defer s.pairingsMu.Unlock()
	if s.learnedPairNetworks == nil {
		s.learnedPairNetworks = map[string]string{}
	}
	if _, exists := s.learnedPairNetworks[credentialKey]; exists {
		// Another goroutine won the race between the read above and this
		// write; keep the first value so two concurrent first-contact
		// messages cannot disagree about what was learned.
		return
	}
	s.learnedPairNetworks[credentialKey] = networkID
}
