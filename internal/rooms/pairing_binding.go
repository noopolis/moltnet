package rooms

import (
	"context"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/observability"
)

func pairCredentialMatchesOrigin(ctx context.Context, claims authn.Claims, originNetworkID string) bool {
	credentialNetworkID := strings.TrimSpace(claims.Network())
	if credentialNetworkID != "" {
		return credentialNetworkID == originNetworkID
	}

	observability.Logger(ctx, "rooms.pairing", "origin_network_id", originNetworkID).
		Warn("accepting inbound pair-scoped message from a pair token with no bound network; bind this credential to a pairing to close cross-pairing impersonation")
	return true
}
