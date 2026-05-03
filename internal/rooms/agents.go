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
	if claims, ok := authn.ClaimsFromContext(ctx); ok && !claims.AllowsAgent(agentID) {
		if claims.AgentToken() {
			return protocol.AgentRegistration{}, agentTokenInvalidForAgentError(agentID)
		}
		return protocol.AgentRegistration{}, agentForbiddenError(
			"requested_agent_id " + agentID + " is not allowed for this token",
		)
	}
	credentialKey, issuedToken, err := registrationCredential(ctx)
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
		CredentialKey: credentialKey,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if existing, ok, err := s.registeredAgent(ctx, agentID); err != nil {
		return protocol.AgentRegistration{}, err
	} else if ok && existing.CredentialKey != registration.CredentialKey {
		if claims, hasClaims := authn.ClaimsFromContext(ctx); hasClaims && claims.Allows(authn.ScopeAdmin) {
			registration.CredentialKey = existing.CredentialKey
			registration.ActorUID = existing.ActorUID
			registration.ActorURI = existing.ActorURI
			registration.CreatedAt = existing.CreatedAt
		} else if strings.TrimSpace(credentialKey) == "anonymous" || issuedToken != "" {
			return protocol.AgentRegistration{}, agentRegisteredError(agentID)
		} else {
			return protocol.AgentRegistration{}, agentConflictError(agentID)
		}
	}

	registered, err := s.registerAgent(ctx, registration)
	if err != nil {
		if errors.Is(err, store.ErrAgentCredential) {
			if issuedToken != "" {
				return protocol.AgentRegistration{}, agentRegisteredError(agentID)
			}
			return protocol.AgentRegistration{}, agentConflictError(agentID)
		}
		return protocol.AgentRegistration{}, err
	}
	if issuedToken != "" {
		registered.AgentToken = issuedToken
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

func (s *Service) AuthenticateAgentTokenContext(
	ctx context.Context,
	token string,
) (authn.Claims, bool, error) {
	if s.agentRegistry == nil || !authn.LooksLikeAgentToken(token) {
		return authn.Claims{}, false, nil
	}
	credentialKey := authn.AgentTokenCredentialKey(token)
	registration, ok, err := s.agentRegistry.GetRegisteredAgentByCredentialKeyContext(ctx, credentialKey)
	if err != nil || !ok {
		return authn.Claims{}, ok, err
	}
	return authn.NewAgentTokenClaims(registration.AgentID, registration.CredentialKey), true, nil
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

func registrationCredential(ctx context.Context) (string, string, error) {
	if credentialKey, ok := credentialKeyFromContext(ctx); ok {
		return credentialKey, "", nil
	}
	if authn.ModeFromContext(ctx) == authn.ModeOpen {
		token, err := authn.GenerateAgentToken()
		if err != nil {
			return "", "", err
		}
		return authn.AgentTokenCredentialKey(token), token, nil
	}
	return "anonymous", "", nil
}

func registrationCredentialKey(ctx context.Context) string {
	if credentialKey, ok := credentialKeyFromContext(ctx); ok {
		return credentialKey
	}
	return "anonymous"
}

func credentialKeyFromContext(ctx context.Context) (string, bool) {
	if claims, ok := authn.ClaimsFromContext(ctx); ok {
		if credentialKey := strings.TrimSpace(claims.CredentialKey); credentialKey != "" {
			return credentialKey, true
		}
		if tokenID := strings.TrimSpace(claims.TokenID); tokenID != "" {
			return authn.StaticCredentialKey(tokenID), true
		}
	}
	return "", false
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
