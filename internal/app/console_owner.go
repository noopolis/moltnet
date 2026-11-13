package app

import (
	"fmt"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// Local owner login is deliberately opt-in and unavailable behind a proxy or
// a non-loopback listener. The browser still receives a normal auth cookie.
func consoleLocalOwnerToken(config Config) (protocol.SecretString, error) {
	if !config.Console.LocalOwnerLogin {
		return "", nil
	}
	if !IsLoopbackListenAddr(config.ListenAddr) || config.Auth.TrustForwardedProto {
		return "", fmt.Errorf("console.local_owner_login requires a loopback listener without a reverse proxy")
	}
	for _, token := range config.Auth.Tokens {
		claims := authn.NewStaticClaims(token)
		if claims.Operator() && claims.Allows(authn.ScopeObserve) && token.Network == "" && len(token.Agents) == 0 {
			return protocol.SecretString(token.Value), nil
		}
	}
	return "", fmt.Errorf("console.local_owner_login requires an unrestricted operator token with observe, write and admin scopes")
}
