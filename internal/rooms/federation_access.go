package rooms

import (
	"context"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) pairingForPairScopedContext(ctx context.Context) (protocol.Pairing, bool) {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) {
		return protocol.Pairing{}, false
	}
	remoteNetworkID := strings.TrimSpace(claims.Network())
	if remoteNetworkID == "" {
		return protocol.Pairing{}, false
	}
	pairing, err := s.findPairingByRemoteNetwork(remoteNetworkID)
	return pairing, err == nil
}

func (s *Service) pairScopedWriteAllowed(ctx context.Context, room protocol.Room) bool {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) || strings.TrimSpace(claims.Network()) == "" {
		return true
	}
	pairing, ok := s.pairingForPairScopedContext(ctx)
	return ok && protocol.RoomFederationAllows(room.Federation, pairing.ID)
}

func (s *Service) roomVisibleToPairScopedContext(ctx context.Context, room protocol.Room) bool {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) || strings.TrimSpace(claims.Network()) == "" {
		return true
	}
	pairing, ok := s.pairingForPairScopedContext(ctx)
	return ok && protocol.RoomFederationAllows(room.Federation, pairing.ID)
}

func (s *Service) filterPairScopedAgentSummaries(ctx context.Context, agents []protocol.AgentSummary) []protocol.AgentSummary {
	if _, scoped := s.pairingForPairScopedContext(ctx); !scoped {
		return agents
	}
	filtered := make([]protocol.AgentSummary, 0, len(agents))
	for _, agent := range agents {
		rooms := make([]string, 0, len(agent.Rooms))
		for _, roomID := range agent.Rooms {
			room, ok, err := s.getRoom(ctx, roomID)
			if err == nil && ok && s.roomVisibleToPairScopedContext(ctx, room) {
				rooms = append(rooms, roomID)
			}
		}
		if len(rooms) == 0 {
			continue
		}
		agent.Rooms = rooms
		filtered = append(filtered, agent)
	}
	return filtered
}
