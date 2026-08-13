package rooms

import (
	"context"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func validateRoomCredentialMode(ctx context.Context, credential protocol.SecretString) error {
	if authn.ModeFromContext(ctx) == authn.ModeNone && strings.TrimSpace(credential.Reveal()) != "" {
		return invalidRoomRequestReasonError("room credential requires auth.mode other than none")
	}
	return nil
}
