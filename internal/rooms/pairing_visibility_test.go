package rooms

import (
	"context"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRestrictedPairingDiscoveryFiltersBeforePagination(t *testing.T) {
	for _, scope := range []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin} {
		t.Run(string(scope), func(t *testing.T) {
			service := newPairingVisibilityService()
			ctx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
				ID:     "luna-viewer",
				Scopes: []authn.Scope{scope},
				Agents: []string{"luna"},
			}))

			roomPage, err := service.PairingRoomsContext(ctx, "pair_1", protocol.PageRequest{Limit: 2})
			if err != nil {
				t.Fatalf("PairingRoomsContext(): %v", err)
			}
			if ids := pairingRoomIDs(roomPage.Rooms); !sameStrings(ids, []string{"private-self", "public"}) || !roomPage.Page.HasMore {
				t.Fatalf("restricted room page = %#v", roomPage)
			}
			nextRooms, err := service.PairingRoomsContext(ctx, "pair_1", protocol.PageRequest{After: "public", Limit: 2})
			if err != nil || !sameStrings(pairingRoomIDs(nextRooms.Rooms), []string{"legacy-self"}) {
				t.Fatalf("next restricted room page = %#v, %v", nextRooms, err)
			}

			agentPage, err := service.PairingAgentsContext(ctx, "pair_1", protocol.PageRequest{Limit: 1})
			if err != nil {
				t.Fatalf("PairingAgentsContext(): %v", err)
			}
			if len(agentPage.Agents) != 1 || agentPage.Agents[0].ID != "local:luna" || !agentPage.Page.HasMore {
				t.Fatalf("restricted agent page = %#v", agentPage)
			}
			if !sameStrings(agentPage.Agents[0].Rooms, []string{"legacy-self", "private-self"}) {
				t.Fatalf("self rooms = %#v", agentPage.Agents[0].Rooms)
			}
			nextAgents, err := service.PairingAgentsContext(ctx, "pair_1", protocol.PageRequest{After: "local:luna", Limit: 10})
			if err != nil || !sameStrings(pairingAgentIDs(nextAgents.Agents), []string{"bob", "public-bot"}) {
				t.Fatalf("next restricted agent page = %#v, %v", nextAgents, err)
			}
		})
	}
}

func TestUnrestrictedPairingDiscoveryPreservesRemoteResults(t *testing.T) {
	service := newPairingVisibilityService()
	rooms, err := service.PairingRooms(context.Background(), "pair_1")
	if err != nil || len(rooms) != 7 {
		t.Fatalf("unrestricted rooms = %#v, %v", rooms, err)
	}
	agents, err := service.PairingAgents(context.Background(), "pair_1")
	if err != nil || len(agents) != 6 {
		t.Fatalf("unrestricted agents = %#v, %v", agents, err)
	}
}

func newPairingVisibilityService() *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		NetworkID: "local",
		Pairings: []protocol.Pairing{{
			ID:              "pair_1",
			RemoteNetworkID: "remote",
			RemoteBaseURL:   "http://remote.example",
			Status:          protocol.PairingStatusConnected,
		}},
		Store:    memory,
		Messages: memory,
		Broker:   events.NewBroker(),
		PairingClient: fakePairingClient{
			network: compatibleRemoteNetwork("remote"),
			rooms: []protocol.Room{
				{ID: "private-hidden", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "private-hidden"), Visibility: protocol.RoomVisibilityPrivate, Members: []string{"local:atlas"}},
				{ID: "private-self", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "private-self"), Visibility: protocol.RoomVisibilityPrivate, Members: []string{"local:luna", "remote:bob"}},
				{ID: "public", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "public"), Visibility: protocol.RoomVisibilityPublic, Members: []string{"public-bot"}},
				{ID: "label-collision", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "label-collision"), Visibility: protocol.RoomVisibilityPrivate, Members: []string{"luna"}},
				{ID: "bad-network", NetworkID: "local", FQID: protocol.RoomFQID("local", "bad-network"), Visibility: protocol.RoomVisibilityPrivate, Members: []string{"local:luna"}},
				{ID: "bad-fqid", NetworkID: "remote", FQID: protocol.RoomFQID("local", "bad-fqid"), Visibility: protocol.RoomVisibilityPrivate, Members: []string{"local:luna"}},
				{ID: "legacy-self", Visibility: protocol.RoomVisibilityPrivate, Members: []string{"local:luna"}},
			},
			agents: []protocol.AgentSummary{
				{ID: "local:atlas", NetworkID: "local", FQID: protocol.AgentFQID("local", "atlas"), Rooms: []string{"private-hidden"}},
				{ID: "local:luna", NetworkID: "local", FQID: protocol.AgentFQID("local", "luna"), Rooms: []string{"private-hidden"}},
				{ID: "bob", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "bob"), Rooms: []string{"private-self"}},
				{ID: "public-bot", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "public-bot"), Rooms: []string{"public"}},
				{ID: "luna", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "luna"), Rooms: []string{"label-collision"}},
				{ID: "local:luna", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "luna"), Rooms: []string{"private-self"}},
			},
		},
	})
}

func pairingRoomIDs(rooms []protocol.Room) []string {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		ids = append(ids, room.ID)
	}
	return ids
}

func pairingAgentIDs(agents []protocol.AgentSummary) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}
	return ids
}
