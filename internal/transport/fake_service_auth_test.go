package transport

import (
	"context"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func (f *fakeService) AuthenticateAgentTokenContext(context.Context, string) (authn.Claims, bool, error) {
	return authn.Claims{}, false, nil
}
