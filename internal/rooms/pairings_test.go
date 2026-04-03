package rooms

import (
	"context"
	"errors"
	"testing"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type fakePairingClient struct {
	network protocol.Network
	rooms   []protocol.Room
	agents  []protocol.AgentSummary
	err     error
}

func (f fakePairingClient) FetchNetwork(ctx context.Context, pairing protocol.Pairing) (protocol.Network, error) {
	if f.err != nil {
		return protocol.Network{}, f.err
	}
	return f.network, nil
}

func (f fakePairingClient) FetchRooms(ctx context.Context, pairing protocol.Pairing) ([]protocol.Room, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rooms, nil
}

func (f fakePairingClient) FetchAgents(ctx context.Context, pairing protocol.Pairing) ([]protocol.AgentSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.agents, nil
}

func (f fakePairingClient) RelayMessage(
	ctx context.Context,
	pairing protocol.Pairing,
	request protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	if f.err != nil {
		return protocol.MessageAccepted{}, f.err
	}
	return protocol.MessageAccepted{
		MessageID: request.ID,
		Accepted:  true,
	}, nil
}

func TestServicePairingLookups(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Pairings: []protocol.Pairing{
			{
				ID:                "pair_1",
				RemoteNetworkID:   "remote",
				RemoteNetworkName: "Remote",
				RemoteBaseURL:     "http://remote.example",
				Status:            "connected",
			},
		},
		Store:    memory,
		Messages: memory,
		Broker:   events.NewBroker(),
		PairingClient: fakePairingClient{
			network: protocol.Network{ID: "remote", Name: "Remote"},
			rooms: []protocol.Room{
				{ID: "research", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "research")},
			},
			agents: []protocol.AgentSummary{
				{ID: "writer", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "writer")},
			},
		},
	})

	network, err := service.PairingNetwork(context.Background(), "pair_1")
	if err != nil || network.ID != "remote" {
		t.Fatalf("PairingNetwork() = %#v, %v", network, err)
	}

	rooms, err := service.PairingRooms(context.Background(), "pair_1")
	if err != nil || len(rooms) != 1 || rooms[0].NetworkID != "remote" {
		t.Fatalf("PairingRooms() = %#v, %v", rooms, err)
	}
	roomPage, err := service.PairingRoomsContext(context.Background(), "pair_1", protocol.PageRequest{
		After: "missing",
		Limit: 1,
	})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got page=%#v err=%v", roomPage, err)
	}

	agents, err := service.PairingAgents(context.Background(), "pair_1")
	if err != nil || len(agents) != 1 || agents[0].NetworkID != "remote" {
		t.Fatalf("PairingAgents() = %#v, %v", agents, err)
	}
	agentPage, err := service.PairingAgentsContext(context.Background(), "pair_1", protocol.PageRequest{
		Before: "missing",
		Limit:  1,
	})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got page=%#v err=%v", agentPage, err)
	}
}

func TestServicePairingErrors(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		NetworkID: "local",
		Store:     memory,
		Messages:  memory,
		Broker:    events.NewBroker(),
		Pairings: []protocol.Pairing{
			{ID: "pair_1", RemoteNetworkID: "remote"},
		},
	})

	if _, err := service.PairingNetwork(context.Background(), "missing"); err == nil {
		t.Fatal("expected missing pairing error")
	}
	if _, err := service.PairingRooms(context.Background(), "pair_1"); err == nil {
		t.Fatal("expected missing pairing client error")
	}
	if _, err := service.PairingAgents(context.Background(), "pair_1"); err == nil {
		t.Fatal("expected missing pairing client error")
	}

	service.pairingClient = fakePairingClient{err: errors.New("remote down")}
	if _, err := service.PairingNetwork(context.Background(), "pair_1"); err == nil {
		t.Fatal("expected pairing client failure")
	}
}
