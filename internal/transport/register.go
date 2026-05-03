package transport

import (
	"net/http"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func handleRegisterAgent(service Service, policy *authn.Policy) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request, err := authenticateRegisterRequest(policy, service, request)
		if err != nil {
			writeAuthError(response, err)
			return
		}

		var payload protocol.RegisterAgentRequest
		if err := decodeJSON(response, request, &payload); err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}
		agent, err := service.RegisterAgentContext(request.Context(), payload)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		status := http.StatusCreated
		if agent.AgentToken == "" && !agent.CreatedAt.Equal(agent.UpdatedAt) {
			status = http.StatusOK
		}
		writeJSON(response, status, agent)
	}
}

func authenticateRegisterRequest(
	policy *authn.Policy,
	verifier agentTokenVerifier,
	request *http.Request,
) (*http.Request, error) {
	if policy == nil || !policy.Enabled() {
		return request, nil
	}
	request = requestWithAuthMode(policy, request)
	if !policy.Open() {
		claims, err := authenticateAnyWithVerifier(policy, verifier, request, []authn.Scope{authn.ScopeAdmin, authn.ScopeAttach})
		if err != nil {
			return request, err
		}
		return request.WithContext(authn.WithClaims(request.Context(), claims)), nil
	}

	token, hasToken, err := authn.RequestToken(request)
	if err != nil || !hasToken {
		return request, err
	}
	claims, ok, err := authenticateBearerToken(policy, verifier, request.Context(), token)
	if err != nil {
		return request, err
	}
	if !ok {
		return request, &authn.Error{Status: http.StatusUnauthorized, Message: "invalid token"}
	}
	if !claims.Allows(authn.ScopeAdmin) && !claims.Allows(authn.ScopeAttach) {
		return request, &authn.Error{Status: http.StatusForbidden, Message: "forbidden"}
	}
	return request.WithContext(authn.WithClaims(request.Context(), claims)), nil
}
