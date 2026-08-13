package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/internal/transport"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestInboundRelayRejectsCredentialBoundToDifferentOriginNetwork(t *testing.T) {
	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		RequirePairNetworkBinding: true,
		NetworkID:                 "local",
		NetworkName:               "Local",
		Version:                   "test",
		Pairings: []protocol.Pairing{
			{ID: "pair-b", RemoteNetworkID: "network-b", Token: "pair-b-credential"},
			{ID: "pair-c", RemoteNetworkID: "network-c", Token: "pair-c-credential"},
		},
		Store:    memory,
		Messages: memory,
		Broker:   events.NewBroker(),
	})
	defer service.Close()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:          "relay-room",
		Members:     []string{"network-c:director"},
		WritePolicy: protocol.RoomWritePolicyMembers,
		Federation:  &protocol.RoomFederation{Mode: protocol.RoomFederationAll},
	}); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{
		ID: "pair-b", Value: "pair-b-credential", Network: "network-b", Scopes: []authn.Scope{authn.ScopePair},
	}}})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	body, err := json.Marshal(protocol.SendMessageRequest{
		ID:     "cross-network-forgery",
		Origin: protocol.MessageOrigin{NetworkID: "network-c", MessageID: "network-c-message"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "relay-room"},
		From:   protocol.Actor{Type: "agent", ID: "director", NetworkID: "network-c"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "forged relay origin"}},
	})
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	inbound := NewInboundHandler(transport.NewHTTPHandler(service, policy))
	response, _ := inbound.serve(context.Background(), frameHeader{
		Type: "req", ID: "cross-network-forgery", Auth: "pair-b-credential", Method: http.MethodPost, Path: "/v1/messages",
	}, body)
	if response.Status == http.StatusAccepted {
		t.Fatal("credential for network-b accepted a network-c origin over relay")
	}
	if got := inboundMessageCount(t, service); got != 0 {
		t.Fatalf("stored message count = %d, want 0", got)
	}
}
