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
		CanSendHuman: canConsoleSendHuman(policy, request, network.Capabilities),
	}
	return network
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
