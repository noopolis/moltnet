package transport

import (
	"net/http"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func networkForRequest(policy *authn.Policy, request *http.Request, network protocol.Network) protocol.Network {
	if policy != nil {
		network.Capabilities.PublicRead = policy.PublicRead()
		network.Capabilities.AgentRegistration = policy.AgentRegistration()
	}
	network.Console = &protocol.NetworkConsole{
		CanManageAutomation: canManageAutomation(request) && network.Capabilities.LocalAutomation,
		CanFollow:           !canConnectAgent(policy, request) && network.Capabilities.PublicRead,
		CanSendHuman:        canConsoleSendHuman(policy, request, network.Capabilities),
		Viewer:              consoleViewer(request),
		CanReadPairings:     canReadPairings(policy, request),
		CanReadDMs:          requestAllowsScopes(policy, request, readScopes),
		CanConnectAgent:     canConnectAgent(policy, request),
	}
	return network
}

func canManageAutomation(request *http.Request) bool {
	claims, ok := authn.ClaimsFromContext(request.Context())
	return ok && claims.Operator() && !claims.AgentToken() && !claims.HasAgentRestriction() && claims.Network() == ""
}

func canConsoleSendHuman(
	policy *authn.Policy,
	request *http.Request,
	capabilities protocol.NetworkCapabilities,
) bool {
	if !capabilities.HumanIngress {
		return false
	}
	if policy == nil || !policy.Enabled() {
		return true
	}

	claims, ok := authn.ClaimsFromContext(request.Context())
	if !ok || claims.AgentToken() {
		return false
	}
	return claims.Allows(authn.ScopeWrite)
}

func consoleViewer(request *http.Request) protocol.ConsoleViewer {
	claims, ok := authn.ClaimsFromContext(request.Context())
	if !ok {
		return protocol.ConsoleViewer{Kind: "anonymous"}
	}
	if claims.AgentToken() {
		ids := claims.AgentIDs()
		if len(ids) == 1 {
			return protocol.ConsoleViewer{Kind: "agent", ID: ids[0]}
		}
		return protocol.ConsoleViewer{Kind: "agent"}
	}
	return protocol.ConsoleViewer{Kind: "token", ID: claims.TokenID}
}

// Public identity discovery keeps valid caller claims even when they lack
// metadata read scopes. Public access already permits this response; dropping
// claims would mislabel a write-only token as anonymous and hide its composer.
func networkDiscoveryAuth(policy *authn.Policy, verifier agentTokenVerifier, next http.HandlerFunc) http.HandlerFunc {
	if policy != nil && policy.Enabled() && policy.PublicRead() {
		return optionalAuthInOpen(policy, verifier, nil, next)
	}
	return publicInOpen(policy, verifier, networkScopes, next)
}
