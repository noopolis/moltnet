package rooms

import (
	"context"
	"errors"
	"strings"
	"unicode"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) RegisterAgentContext(
	ctx context.Context,
	request protocol.RegisterAgentRequest,
) (protocol.AgentRegistration, error) {
	agentID, err := s.resolveRequestedAgentID(ctx, request)
	if err != nil {
		return protocol.AgentRegistration{}, err
	}

	now := s.now().UTC()
	registration := protocol.AgentRegistration{
		NetworkID:     s.networkID,
		AgentID:       agentID,
		ActorUID:      s.nextID("actor"),
		ActorURI:      protocol.AgentFQID(s.networkID, agentID),
		DisplayName:   strings.TrimSpace(request.Name),
		CredentialKey: registrationCredentialKey(ctx),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	registered, err := s.registerAgent(ctx, registration)
	if err != nil {
		if errors.Is(err, store.ErrAgentCredential) {
			return protocol.AgentRegistration{}, agentConflictError(agentID)
		}
		return protocol.AgentRegistration{}, err
	}

	return registered, nil
}

func (s *Service) registerAgent(
	ctx context.Context,
	registration protocol.AgentRegistration,
) (protocol.AgentRegistration, error) {
	if s.agentRegistry == nil {
		return registration, nil
	}
	return s.agentRegistry.RegisterAgentContext(ctx, registration)
}

func (s *Service) registeredAgent(
	ctx context.Context,
	agentID string,
) (protocol.AgentRegistration, bool, error) {
	if s.agentRegistry == nil {
		return protocol.AgentRegistration{}, false, nil
	}
	return s.agentRegistry.GetRegisteredAgentContext(ctx, strings.TrimSpace(agentID))
}

func (s *Service) resolveRequestedAgentID(
	ctx context.Context,
	request protocol.RegisterAgentRequest,
) (string, error) {
	if agentID := strings.TrimSpace(request.RequestedAgentID); agentID != "" {
		if err := protocol.ValidateMemberID(agentID); err != nil {
			return "", invalidMessageRequestError("requested_agent_id " + err.Error())
		}
		return agentID, nil
	}

	base := slugAgentID(request.Name)
	for attempt := 0; attempt < 100; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = base + "-" + shortSuffix(s.nextID("agent"))
		}
		if _, ok, err := s.registeredAgent(ctx, candidate); err != nil {
			return "", err
		} else if !ok {
			return candidate, nil
		}
	}

	return "", invalidMessageRequestError("unable to generate unique agent id")
}

func registrationCredentialKey(ctx context.Context) string {
	if claims, ok := authn.ClaimsFromContext(ctx); ok {
		if tokenID := strings.TrimSpace(claims.TokenID); tokenID != "" {
			return "token:" + tokenID
		}
	}
	return "anonymous"
}

func slugAgentID(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range lower {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		case r == '_' || r == '.' || r == '-':
			if !lastDash {
				builder.WriteRune('-')
				lastDash = true
			}
		case unicode.IsSpace(r):
			if !lastDash {
				builder.WriteRune('-')
				lastDash = true
			}
		default:
			if !lastDash {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}

	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "agent"
	}
	return slug
}

func shortSuffix(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 8 {
		return trimmed
	}
	return trimmed[len(trimmed)-8:]
}
