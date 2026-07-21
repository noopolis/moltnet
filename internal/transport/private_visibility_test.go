package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// Keep the legacy fake's context-free lookup for older tests, while making the
// production Service contract context-aware. Tests which need to observe the
// request context can replace this receiver in their own fixture.
func (f *fakeService) GetDirectConversationContext(_ context.Context, dmID string) (protocol.DirectConversation, error) {
	return f.GetDirectConversation(dmID)
}

func TestBearerAttachmentReceivesReplayGapWithoutPrivateLeak(t *testing.T) {
	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{AllowHumanIngress: true, NetworkID: "local", NetworkName: "Local", Version: "test", Store: memory, Messages: memory, Broker: events.NewBroker()})
	for _, room := range []protocol.CreateRoomRequest{{ID: "public", Visibility: protocol.RoomVisibilityPublic}, {ID: "own", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate}, {ID: "other", Members: []string{"atlas"}, Visibility: protocol.RoomVisibilityPrivate}} {
		if _, err := service.CreateRoom(room); err != nil {
			t.Fatal(err)
		}
	}
	staticToken := authn.TokenConfig{ID: "luna-attach", Value: "luna-secret", Scopes: []authn.Scope{authn.ScopeAttach}, Agents: []string{"luna"}}
	staticClaims := authn.NewStaticClaims(staticToken)
	registration, err := service.RegisterAgentContext(authn.WithClaims(context.Background(), staticClaims), protocol.RegisterAgentRequest{RequestedAgentID: "luna"})
	if err != nil {
		t.Fatalf("RegisterAgentContext() = %#v, %v", registration, err)
	}
	send := func(target protocol.Target, text string, from string) protocol.MessageAccepted {
		accepted, sendErr := service.SendMessage(protocol.SendMessageRequest{Target: target, From: protocol.Actor{Type: "human", ID: from}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: text}}})
		if sendErr != nil {
			t.Fatal(sendErr)
		}
		return accepted
	}
	send(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}, "history-cursor", "luna")
	send(protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-own", ParticipantIDs: []string{"luna", "mira"}}, "replay-own-dm", "luna")
	send(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "public"}, "replay-public", "luna")
	send(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}, "replay-hidden-room", "atlas")
	send(protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-other", ParticipantIDs: []string{"atlas", "mira"}}, "replay-hidden-dm", "atlas")
	replaySentinel := send(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}, "replay-allowed-sentinel", "luna")

	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, PublicRead: false, Tokens: []authn.TokenConfig{staticToken}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHTTPHandler(service, policy))
	defer server.Close()
	gapObserved := false
	dial := func(token, cursor string) *websocket.Conn {
		headers := http.Header{}
		headers.Set("Authorization", "Bearer "+token)
		endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/attach"
		connection, _, dialErr := websocket.DefaultDialer.Dial(endpoint, headers)
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		var hello protocol.AttachmentFrame
		if readErr := connection.ReadJSON(&hello); readErr != nil {
			t.Fatal(readErr)
		}
		if writeErr := connection.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpIdentify, Version: protocol.AttachmentProtocolV1, NetworkID: "local", Cursor: cursor, Agent: &protocol.Actor{ID: "luna"}}); writeErr != nil {
			t.Fatal(writeErr)
		}
		var ready protocol.AttachmentFrame
		if readErr := connection.ReadJSON(&ready); readErr != nil || ready.Op != protocol.AttachmentOpReady {
			t.Fatalf("attachment ready = %#v, %v", ready, readErr)
		}
		return connection
	}
	readUntil := func(connection *websocket.Conn, sentinel string) (map[string]bool, string) {
		seen := make(map[string]bool)
		var cursor string
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if err := connection.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			var frame protocol.AttachmentFrame
			if err := connection.ReadJSON(&frame); err != nil {
				continue
			}
			if frame.Op != protocol.AttachmentOpEvent || frame.Event == nil {
				continue
			}
			if frame.Event.Type == protocol.EventTypeReplayGap {
				gapObserved = true
			}
			cursor = frame.Event.ID
			if frame.Event.Message != nil && len(frame.Event.Message.Parts) > 0 {
				seen[frame.Event.Message.Parts[0].Text] = true
			}
			if seen[sentinel] {
				return seen, cursor
			}
		}
		t.Fatalf("attachment did not reach %q; saw %#v", sentinel, seen)
		return nil, ""
	}
	connection := dial(staticToken.Value, "cursor_does_not_exist")
	seen, replayCursor := readUntil(connection, "replay-allowed-sentinel")
	if !gapObserved {
		t.Fatal("attachment did not positively observe replay gap")
	}
	for _, want := range []string{"replay-own-dm", "replay-public", "replay-allowed-sentinel"} {
		if !seen[want] {
			t.Fatalf("missing allowed replay %q: %#v", want, seen)
		}
	}
	for _, blocked := range []string{"replay-hidden-room", "replay-hidden-dm"} {
		if seen[blocked] {
			t.Fatalf("attachment leaked %q", blocked)
		}
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpAck, Version: protocol.AttachmentProtocolV1, Cursor: replayCursor}); err != nil {
		t.Fatal(err)
	}
	send(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "public"}, "first-public-live", "luna")
	send(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}, "first-own-live", "luna")
	seen, _ = readUntil(connection, "first-own-live")
	if !seen["first-public-live"] {
		t.Fatal("attachment missed public live event")
	}
	_ = connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	connection.Close()

	connection = dial(staticToken.Value, replaySentinel.EventID)
	seen, _ = readUntil(connection, "first-own-live")
	if !seen["first-own-live"] || seen["replay-hidden-room"] || seen["replay-hidden-dm"] {
		t.Fatalf("reconnect replay visibility = %#v", seen)
	}
	send(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}, "reconnect-live-sentinel", "luna")
	seen, _ = readUntil(connection, "reconnect-live-sentinel")
	if !seen["reconnect-live-sentinel"] {
		t.Fatal("reconnect missed filtered live sentinel")
	}
	connection.Close()

}

func TestRealAttachmentFiltersPrivateReplayAndLiveEvents(t *testing.T) {
	TestBearerAttachmentReceivesReplayGapWithoutPrivateLeak(t)
}

func TestAttachmentRemoteSameNameRemovalDoesNotDisconnect(t *testing.T) {
	stream := make(chan protocol.Event, 3)
	service := &fakeService{network: protocol.Network{ID: "local"}, rooms: []protocol.Room{{ID: "public", Visibility: protocol.RoomVisibilityPublic}}, stream: stream}
	policy := mustBearerPolicy(t, authn.TokenConfig{ID: "attach", Value: "secret", Scopes: []authn.Scope{authn.ScopeAttach}, Agents: []string{"luna"}})
	server := httptest.NewServer(NewHTTPHandler(service, policy))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/attach"
	headers := http.Header{"Authorization": []string{"Bearer secret"}}
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var frame protocol.AttachmentFrame
	if err := connection.ReadJSON(&frame); err != nil || frame.Op != protocol.AttachmentOpHello {
		t.Fatalf("hello = %#v, %v", frame, err)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpIdentify, Version: protocol.AttachmentProtocolV1, NetworkID: "local", Agent: &protocol.Actor{ID: "luna"}}); err != nil {
		t.Fatal(err)
	}
	if err := connection.ReadJSON(&frame); err != nil || frame.Op != protocol.AttachmentOpReady {
		t.Fatalf("ready = %#v, %v", frame, err)
	}
	stream <- protocol.Event{ID: "remote-remove", NetworkID: "remote", Type: protocol.EventTypeAgentRemoved, Agent: &protocol.AgentEvent{AgentID: "luna", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "luna")}}
	stream <- protocol.Event{ID: "sentinel", NetworkID: "local", Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "public"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "allowed-sentinel"}}}}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := connection.ReadJSON(&frame); err != nil || frame.Event == nil || frame.Event.ID != "sentinel" {
		t.Fatalf("remote removal affected attachment: %#v, %v", frame, err)
	}
	stream <- protocol.Event{ID: "local-remove", NetworkID: "local", Type: protocol.EventTypeAgentRemoved, Agent: &protocol.AgentEvent{AgentID: "luna", NetworkID: "local", FQID: protocol.AgentFQID("local", "luna")}}
	if err := connection.ReadJSON(&frame); err != nil || frame.Op != protocol.AttachmentOpError || !strings.Contains(frame.Error, "removed") {
		t.Fatalf("local removal did not close attachment: %#v, %v", frame, err)
	}
}
