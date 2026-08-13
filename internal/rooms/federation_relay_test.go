package rooms

import (
	"context"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPairingRelayBlocksNonFederatedRoom(t *testing.T) {
	t.Parallel()

	client := newFederationRelayClient()
	service := newFederationRelayTestService(client)
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "private", Federation: protocol.DefaultRoomFederation()}); err != nil {
		t.Fatalf("CreateRoom(private): %v", err)
	}

	if _, err := service.SendMessage(roomSend("private", "local")); err != nil {
		t.Fatalf("SendMessage(): %v", err)
	}
	select {
	case request := <-client.relayed:
		t.Fatalf("non-federated room relayed %#v", request)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPairingRelayPreservesOriginForFederatedRoom(t *testing.T) {
	t.Parallel()

	client := newFederationRelayClient()
	service := newFederationRelayTestService(client)
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID: "shared", Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_remote"}},
	}); err != nil {
		t.Fatalf("CreateRoom(shared): %v", err)
	}
	request := roomSend("shared", "local")
	request.ID = "msg_origin"
	request.Origin = protocol.MessageOrigin{NetworkID: "local", MessageID: "origin-message"}
	if _, err := service.SendMessage(request); err != nil {
		t.Fatalf("SendMessage(): %v", err)
	}
	select {
	case relayed := <-client.relayed:
		if relayed.Origin != request.Origin {
			t.Fatalf("relayed origin = %#v, want %#v", relayed.Origin, request.Origin)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for federated room relay")
	}
}

type federationRelayClient struct {
	relayed chan protocol.SendMessageRequest
}

func newFederationRelayClient() *federationRelayClient {
	return &federationRelayClient{relayed: make(chan protocol.SendMessageRequest, 1)}
}

func (c *federationRelayClient) FetchNetwork(context.Context, protocol.Pairing) (protocol.Network, error) {
	return compatibleRemoteNetwork("remote"), nil
}

func (c *federationRelayClient) FetchRooms(context.Context, protocol.Pairing) ([]protocol.Room, error) {
	return nil, nil
}

func (c *federationRelayClient) FetchAgents(context.Context, protocol.Pairing) ([]protocol.AgentSummary, error) {
	return nil, nil
}

func (c *federationRelayClient) RelayMessage(_ context.Context, _ protocol.Pairing, request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	c.relayed <- request
	return protocol.MessageAccepted{MessageID: request.ID, Accepted: true}, nil
}

func newFederationRelayTestService(client PairingClient) *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		NetworkID: "local",
		Pairings: []protocol.Pairing{{
			ID: "pair_remote", RemoteNetworkID: "remote", RemoteBaseURL: "https://remote.example",
			Status: protocol.PairingStatusConnected, Diagnostics: &protocol.PairingDiagnostics{},
		}},
		Store: memory, Messages: memory, Broker: events.NewBroker(), PairingClient: client,
	})
}
