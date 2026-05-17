package transport

import (
	"context"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func (f *fakeService) AuthenticateAgentTokenContext(_ context.Context, token string) (authn.Claims, bool, error) {
	if f.agentTokenClaims != nil {
		claims, ok := f.agentTokenClaims[token]
		return claims, ok, nil
	}
	return authn.Claims{}, false, nil
}
