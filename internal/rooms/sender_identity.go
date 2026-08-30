package rooms

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// This file holds SendMessageContext's sender-identity validation: whether a
// message's claimed actor is who its credential says it is, including the
// remote-origin/pair-credential cases. Split out of messaging.go (F4, review
// round 2) to keep messaging.go under the repo's 400-line limit; no behavior
// changed in the split.

func (s *Service) senderCredentialBound(ctx context.Context, actor protocol.Actor) bool {
	if strings.TrimSpace(actor.Type) == "human" || strings.TrimSpace(actor.NetworkID) != s.networkID {
		return false
	}
	agentID, local := s.agentCollisionID(actor)
	if !local || agentID == "" || strings.TrimSpace(actor.ID) != agentID ||
		strings.TrimSpace(actor.FQID) != protocol.AgentFQID(s.networkID, agentID) {
		return false
	}
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopeWrite) || claims.Allows(authn.ScopePair) {
		return false
	}
	agentIDs := claims.AgentIDs()
	if len(agentIDs) != 1 || strings.TrimSpace(agentIDs[0]) != agentID {
		return false
	}
	registration, ok, err := s.registeredAgent(ctx, agentID)
	if err != nil || !ok {
		return false
	}
	credentialKey := strings.TrimSpace(claims.CredentialKey)
	return credentialKey != "" && credentialKey == strings.TrimSpace(registration.CredentialKey)
}

func (s *Service) validateSenderIdentity(ctx context.Context, actor protocol.Actor, origin protocol.MessageOrigin) error {
	if s.remoteOriginRequiresPairCheck(ctx, actor, origin) {
		return agentConflictError("remote origin actor")
	}
	if strings.TrimSpace(actor.Type) == "human" {
		return s.validateHumanSender(ctx, origin)
	}
	switch s.remoteOriginPairStatus(ctx, actor, origin) {
	case remoteOriginPaired:
		return nil
	case remoteOriginPairUnbound:
		// F3 (confirmed live): falling through to the generic
		// agentCollisionID/agentConflictError path below for this case
		// produced a wildly misleading 409 "agent %q is already registered
		// with different credentials" -- there is no agent registration
		// conflict here at all, just a pair credential auth.
		// require_pair_network_binding refused for having no confirmed
		// network binding (pairCredentialMatchesOrigin,
		// pairing_binding.go). Say that instead.
		return agentForbiddenError(fmt.Sprintf(
			"remote-origin agent %q requires a pair credential bound to network %q (auth.require_pair_network_binding is enabled)",
			strings.TrimSpace(actor.ID), strings.TrimSpace(origin.NetworkID)))
	}
	agentID, local := s.agentCollisionID(actor)
	if agentID == "" {
		return nil
	}
	mode := authn.ModeFromContext(ctx)
	claims, hasClaims := authn.ClaimsFromContext(ctx)
	if mode == authn.ModeOpen && !local {
		return agentForbiddenError(fmt.Sprintf("remote-origin agent %q requires pair scope", agentID))
	}
	if !local && hasClaims {
		return agentConflictError(agentID)
	}
	if s.agentRegistry == nil {
		return nil
	}
	if hasClaims {
		if claims.Allows(authn.ScopePair) && !claims.Allows(authn.ScopeWrite) {
			return agentForbiddenError("pair tokens cannot assert local agents")
		}
		if !claims.AllowsAgent(agentID) {
			if claims.AgentToken() {
				return agentTokenInvalidForAgentError(agentID)
			}
			return agentForbiddenError(fmt.Sprintf("agent %q is not allowed for this token", agentID))
		}
	}

	registration, ok, err := s.registeredAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if !ok {
		if mode == authn.ModeOpen {
			return agentRegistrationRequiredError(agentID)
		}
		return nil
	}
	credentialKey := registrationCredentialKey(ctx)
	if strings.TrimSpace(credentialKey) == "anonymous" {
		if mode == authn.ModeOpen {
			return agentRequiresTokenError(agentID)
		}
		if registration.CredentialKey == credentialKey {
			return nil
		}
	}
	if registration.CredentialKey == credentialKey {
		if mode == authn.ModeOpen && (!hasClaims || !claims.Allows(authn.ScopeWrite)) {
			return agentForbiddenError(fmt.Sprintf("agent %q requires write scope", agentID))
		}
		return nil
	}
	if hasClaims && claims.Operator() {
		return nil
	}
	if hasClaims && claims.AgentToken() {
		return agentTokenInvalidForAgentError(agentID)
	}
	if mode == authn.ModeOpen && !hasClaims {
		return agentRequiresTokenError(agentID)
	}
	if mode == authn.ModeOpen && hasClaims && !claims.Allows(authn.ScopeWrite) {
		return agentForbiddenError(fmt.Sprintf("agent %q requires write scope", agentID))
	}
	if mode == authn.ModeOpen {
		return agentConflictError(agentID)
	}
	if registration.CredentialKey != credentialKey {
		return agentConflictError(agentID)
	}
	return nil
}

func (s *Service) remoteOriginRequiresPairCheck(
	ctx context.Context,
	actor protocol.Actor,
	origin protocol.MessageOrigin,
) bool {
	originNetworkID := strings.TrimSpace(origin.NetworkID)
	if originNetworkID == "" || originNetworkID == s.networkID {
		return false
	}
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) {
		return false
	}
	return !actorHasExplicitConsistentNetworkID(actor, originNetworkID)
}

func (s *Service) validateHumanSender(ctx context.Context, origin protocol.MessageOrigin) error {
	remoteOrigin := false
	if originNetworkID := strings.TrimSpace(origin.NetworkID); originNetworkID != "" && originNetworkID != s.networkID {
		remoteOrigin = true
		claims, ok := authn.ClaimsFromContext(ctx)
		if !ok || !claims.Allows(authn.ScopePair) {
			return agentForbiddenError("remote-origin human sender requires pair scope")
		}
		if !s.pairCredentialMatchesOrigin(ctx, claims, originNetworkID, s.requirePairNetworkBinding) {
			return agentForbiddenError("remote-origin human sender requires a pair credential bound to this network")
		}
	}
	if authn.ModeFromContext(ctx) == authn.ModeNone {
		return nil
	}
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok {
		return &Error{
			status: http.StatusUnauthorized,
			msg:    "human ingress requires a write token",
			cause:  ErrAgentUnauthorized,
		}
	}
	if remoteOrigin && claims.Allows(authn.ScopePair) {
		return nil
	}
	if claims.AgentToken() || !claims.StaticToken() || !claims.Allows(authn.ScopeWrite) {
		return agentForbiddenError("human ingress requires a static write token")
	}
	return nil
}

// remoteOriginPairStatusKind is remoteOriginPairStatus's result: whether a
// non-human remote-origin actor is a properly paired sender, is a pair
// credential that failed only the network-binding check (F3's accurate
// 403, not the generic agent-conflict fallthrough), or neither applies (the
// caller falls through to ordinary local-agent identity handling).
type remoteOriginPairStatusKind int

const (
	remoteOriginNotPairScoped remoteOriginPairStatusKind = iota
	remoteOriginPaired
	remoteOriginPairUnbound
)

// remoteOriginPairStatus is isPairedRemoteOriginActor's replacement: it
// still reports whether actor is a fully paired remote-origin sender
// (remoteOriginPaired, same conditions as before), but also distinguishes
// the case that used to collapse into the same "false" as "not a pair
// credential at all" -- a pair-scoped, origin-consistent credential that
// pairCredentialMatchesOrigin refused specifically for lacking a confirmed
// network binding (remoteOriginPairUnbound). validateSenderIdentity gives
// that case its own accurate error instead of falling through to
// agentConflictError's misleading "already registered with different
// credentials" 409.
func (s *Service) remoteOriginPairStatus(ctx context.Context, actor protocol.Actor, origin protocol.MessageOrigin) remoteOriginPairStatusKind {
	originNetworkID := strings.TrimSpace(origin.NetworkID)
	if originNetworkID == "" || originNetworkID == s.networkID {
		return remoteOriginNotPairScoped
	}
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) {
		return remoteOriginNotPairScoped
	}
	if !actorHasExplicitConsistentNetworkID(actor, originNetworkID) {
		return remoteOriginNotPairScoped
	}
	if s.pairCredentialMatchesOrigin(ctx, claims, originNetworkID, s.requirePairNetworkBinding) {
		return remoteOriginPaired
	}
	return remoteOriginPairUnbound
}

func (s *Service) agentCollisionID(actor protocol.Actor) (string, bool) {
	if networkID, agentID, ok := protocol.ParseScopedAgentID(actor.ID); ok {
		return strings.TrimSpace(agentID), strings.TrimSpace(networkID) == s.networkID
	}
	if networkID, agentID, ok := protocol.ParseAgentFQID(actor.ID); ok {
		return strings.TrimSpace(agentID), strings.TrimSpace(networkID) == s.networkID
	}
	if networkID, _, ok := protocol.ParseAgentFQID(actor.FQID); ok && strings.TrimSpace(networkID) != s.networkID {
		return strings.TrimSpace(actor.ID), false
	}
	if networkID := strings.TrimSpace(actor.NetworkID); networkID != "" && networkID != s.networkID {
		return strings.TrimSpace(actor.ID), false
	}
	return strings.TrimSpace(actor.ID), true
}

func actorHasExplicitConsistentNetworkID(actor protocol.Actor, networkID string) bool {
	if strings.TrimSpace(actor.NetworkID) != networkID {
		return false
	}
	if parsedNetworkID, _, ok := protocol.ParseAgentFQID(actor.FQID); ok &&
		strings.TrimSpace(parsedNetworkID) != networkID {
		return false
	}
	if parsedNetworkID, _, ok := protocol.ParseScopedAgentID(actor.ID); ok &&
		strings.TrimSpace(parsedNetworkID) != networkID {
		return false
	}
	if parsedNetworkID, _, ok := protocol.ParseAgentFQID(actor.ID); ok &&
		strings.TrimSpace(parsedNetworkID) != networkID {
		return false
	}
	return true
}
