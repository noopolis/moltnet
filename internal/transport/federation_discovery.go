package transport

import (
	"context"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// filterPairScopedRoomDiscovery is a serving-side guard for FetchRooms. The
// room service applies the same filter before pagination; keeping this check at
// the HTTP boundary prevents another Service implementation from exposing a
// room to a peer whose bound pairing is not federated with it.
func filterPairScopedRoomDiscovery(ctx context.Context, service Service, page protocol.RoomPage) (protocol.RoomPage, error) {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) {
		return page, nil
	}

	remoteNetworkID := strings.TrimSpace(claims.Network())
	if remoteNetworkID == "" {
		page.Rooms = nil
		return page, nil
	}
	pairings, err := service.ListPairingsContext(ctx, protocol.PageRequest{})
	if err != nil {
		return protocol.RoomPage{}, err
	}
	for _, pairing := range pairings.Pairings {
		if strings.TrimSpace(pairing.RemoteNetworkID) != remoteNetworkID {
			continue
		}
		rooms := make([]protocol.Room, 0, len(page.Rooms))
		for _, room := range page.Rooms {
			if protocol.RoomFederationAllows(room.Federation, pairing.ID) {
				rooms = append(rooms, room)
			}
		}
		page.Rooms = rooms
		return page, nil
	}

	page.Rooms = nil
	return page, nil
}
