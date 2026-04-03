package rooms

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/pairings"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestServiceRelaysRoomMessagesAcrossPairings(t *testing.T) {
	t.Parallel()

	serviceA, serverA := newRelayTestService(t, "net_a", "Net A")
	defer serverA.Close()
	serviceB, serverB := newRelayTestService(t, "net_b", "Net B")
	defer serverB.Close()

	serviceA.pairings = []protocol.Pairing{{
		ID:              "pair_b",
		RemoteNetworkID: "net_b",
		RemoteBaseURL:   serverB.URL,
		Status:          "connected",
	}}
	serviceB.pairings = []protocol.Pairing{{
		ID:              "pair_a",
		RemoteNetworkID: "net_a",
		RemoteBaseURL:   serverA.URL,
		Status:          "connected",
	}}

	for _, service := range []*Service{serviceA, serviceB} {
		if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research", Members: []string{"alpha", "beta"}}); err != nil {
			t.Fatalf("CreateRoom() error = %v", err)
		}
	}

	accepted, err := serviceA.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: "text", Text: "relay this to net_b"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if accepted.MessageID == "" || accepted.MessageID[:10] != "msg_net_a_" {
		t.Fatalf("unexpected accepted id %#v", accepted)
	}

	pageA, err := serviceA.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages(net_a) error = %v", err)
	}
	if len(pageA.Messages) != 1 {
		t.Fatalf("expected one local message, got %#v", pageA)
	}

	pageB := waitForRoomMessage(t, serviceB, "research")

	message := pageB.Messages[0]
	if message.ID != accepted.MessageID {
		t.Fatalf("expected relayed message id %q, got %#v", accepted.MessageID, message)
	}
	if message.Origin.NetworkID != "net_a" || message.Origin.MessageID != accepted.MessageID {
		t.Fatalf("unexpected message origin %#v", message.Origin)
	}
	if message.From.NetworkID != "net_a" || message.From.FQID != protocol.AgentFQID("net_a", "alpha") {
		t.Fatalf("unexpected relayed sender %#v", message.From)
	}

	pageAAfter, err := serviceA.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages(net_a second) error = %v", err)
	}
	if len(pageAAfter.Messages) != 1 {
		t.Fatalf("expected relay loop prevention, got %#v", pageAAfter)
	}
}

func TestServiceRelaysScopedDirectMessagesAcrossPairings(t *testing.T) {
	t.Parallel()

	serviceA, serverA := newRelayTestService(t, "net_a", "Net A")
	defer serverA.Close()
	serviceB, serverB := newRelayTestService(t, "net_b", "Net B")
	defer serverB.Close()

	serviceA.pairings = []protocol.Pairing{{
		ID:              "pair_b",
		RemoteNetworkID: "net_b",
		RemoteBaseURL:   serverB.URL,
		Status:          "connected",
	}}
	serviceB.pairings = []protocol.Pairing{{
		ID:              "pair_a",
		RemoteNetworkID: "net_a",
		RemoteBaseURL:   serverA.URL,
		Status:          "connected",
	}}

	_, err := serviceA.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind: protocol.TargetKindDM,
			DMID: "dm_alpha_gamma",
			ParticipantIDs: []string{
				protocol.ScopedAgentID("net_a", "alpha"),
				protocol.ScopedAgentID("net_b", "gamma"),
			},
		},
		From:  protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{{Kind: "text", Text: "ping remote gamma"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() dm error = %v", err)
	}

	pageB := waitForDMMessage(t, serviceB, "dm_alpha_gamma")

	message := pageB.Messages[0]
	if len(message.Target.ParticipantIDs) != 2 || message.Target.ParticipantIDs[0] != "net_a:alpha" || message.Target.ParticipantIDs[1] != "net_b:gamma" {
		t.Fatalf("unexpected relayed participants %#v", message.Target.ParticipantIDs)
	}
	if message.From.NetworkID != "net_a" || message.Origin.NetworkID != "net_a" {
		t.Fatalf("unexpected relayed dm metadata %#v", message)
	}

	conversations, err := serviceB.ListDirectConversations()
	if err != nil {
		t.Fatalf("ListDirectConversations() error = %v", err)
	}
	if len(conversations.DMs) != 1 || len(conversations.DMs[0].ParticipantIDs) != 2 || conversations.DMs[0].ParticipantIDs[0] != "net_a:alpha" || conversations.DMs[0].ParticipantIDs[1] != "net_b:gamma" {
		t.Fatalf("unexpected relayed conversations %#v", conversations)
	}
}

func newRelayTestService(t *testing.T, networkID string, networkName string) (*Service, *httptest.Server) {
	t.Helper()

	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         networkID,
		NetworkName:       networkName,
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
		PairingClient:     pairings.NewClient(),
	})

	server := httptest.NewServer(newRelayTestHandler(service))
	return service, server
}

func newRelayTestHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/messages" {
			http.NotFound(response, request)
			return
		}

		var payload protocol.SendMessageRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}

		accepted, err := service.SendMessageContext(request.Context(), payload)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadGateway)
			return
		}

		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(response).Encode(accepted)
	})
}

func waitForRoomMessage(t *testing.T, service *Service, roomID string) protocol.MessagePage {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		page, err := service.ListRoomMessages(roomID, "", 10)
		if err == nil && len(page.Messages) > 0 {
			return page
		}
		time.Sleep(20 * time.Millisecond)
	}

	page, err := service.ListRoomMessages(roomID, "", 10)
	t.Fatalf("timed out waiting for room relay, page=%#v err=%v", page, err)
	return protocol.MessagePage{}
}

func waitForDMMessage(t *testing.T, service *Service, dmID string) protocol.MessagePage {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		page, err := service.ListDMMessages(dmID, "", 10)
		if err == nil && len(page.Messages) > 0 {
			return page
		}
		time.Sleep(20 * time.Millisecond)
	}

	page, err := service.ListDMMessages(dmID, "", 10)
	t.Fatalf("timed out waiting for dm relay, page=%#v err=%v", page, err)
	return protocol.MessagePage{}
}
