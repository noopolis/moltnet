package picoclaw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type streamerStub struct {
	events []protocol.Event
	err    error
}

func (s streamerStub) StreamEvents(
	_ context.Context,
	_ bridgeconfig.Config,
	handle func(event protocol.Event) error,
) error {
	for _, event := range s.events {
		if err := handle(event); err != nil {
			return err
		}
	}
	return s.err
}

type backoffStub struct{}

func (backoffStub) Delay(int) time.Duration {
	return 0
}

func TestRunEventLoopDeliversBootstrapAndMessage(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	received := make(chan picoEnvelope, 4)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/pico/ws" {
			http.NotFound(writer, request)
			return
		}
		if got := request.Header.Get("Authorization"); got != "Bearer pico-secret" {
			t.Fatalf("unexpected pico authorization header %q", got)
		}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		var payload picoEnvelope
		if err := conn.ReadJSON(&payload); err != nil {
			t.Fatalf("read websocket payload: %v", err)
		}
		payload.SessionID = request.URL.Query().Get("session_id")
		received <- payload
	}))
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "reviewer", Name: "Reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://moltnet",
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			EventsURL: strings.Replace(server.URL, "http://", "ws://", 1) + "/pico/ws",
			Token:     "pico-secret",
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto},
		},
	}

	event := protocol.Event{
		ID:        "evt_123",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_123",
			NetworkID: "local",
			Target: protocol.Target{
				Kind:   protocol.TargetKindRoom,
				RoomID: "research",
			},
			From: protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Parts: []protocol.Part{
				{Kind: protocol.PartKindText, Text: "Review this patch"},
			},
		},
	}

	if err := runEventLoop(context.Background(), config, streamerStub{events: []protocol.Event{event}}, backoffStub{}); err != nil {
		t.Fatalf("runEventLoop() error = %v", err)
	}

	var messages []picoEnvelope
	for len(messages) < 2 {
		select {
		case payload := <-received:
			messages = append(messages, payload)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for pico bridge dispatches, got %d", len(messages))
		}
	}

	bootstrap := messages[0]
	if bootstrap.Type != "message.send" {
		t.Fatalf("unexpected bootstrap type %q", bootstrap.Type)
	}
	if bootstrap.SessionID != "agent:reviewer:local:room:research" {
		t.Fatalf("unexpected bootstrap session id %q", bootstrap.SessionID)
	}
	if !strings.Contains(bootstrap.Payload["content"].(string), "Channel: moltnet") {
		t.Fatalf("expected bootstrap channel context, got %q", bootstrap.Payload["content"])
	}
	if !strings.Contains(bootstrap.Payload["content"].(string), "Chat ID: local:room:research") {
		t.Fatalf("expected bootstrap chat id, got %q", bootstrap.Payload["content"])
	}
	if !strings.Contains(bootstrap.Payload["content"].(string), "Moltnet conversation attached. Use the `moltnet` skill in this conversation.") {
		t.Fatalf("expected bootstrap payload, got %q", bootstrap.Payload["content"])
	}

	inbound := messages[1]
	if inbound.Type != "message.send" {
		t.Fatalf("unexpected inbound type %q", inbound.Type)
	}
	if inbound.SessionID != "agent:reviewer:local:room:research" {
		t.Fatalf("unexpected inbound session id %q", inbound.SessionID)
	}
	content := inbound.Payload["content"].(string)
	if !strings.Contains(content, "Channel: moltnet") {
		t.Fatalf("expected message channel context, got %q", content)
	}
	if !strings.Contains(content, "Chat ID: local:room:research") {
		t.Fatalf("expected room chat id in message payload, got %q", content)
	}
	if !strings.Contains(content, "From: local/agent/writer") {
		t.Fatalf("expected sender metadata in message payload, got %q", content)
	}
	if !strings.Contains(content, "Message:\nReview this patch") {
		t.Fatalf("expected plain message body in payload, got %q", content)
	}
}

func TestRunEventLoopSkipsBootstrapForMentionOnlyBindings(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	received := make(chan picoEnvelope, 2)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/pico/ws" {
			http.NotFound(writer, request)
			return
		}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		var payload picoEnvelope
		if err := conn.ReadJSON(&payload); err != nil {
			t.Fatalf("read websocket payload: %v", err)
		}
		payload.SessionID = request.URL.Query().Get("session_id")
		received <- payload
	}))
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "reviewer", Name: "Reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://moltnet",
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			EventsURL: strings.Replace(server.URL, "http://", "ws://", 1) + "/pico/ws",
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto},
		},
	}

	event := protocol.Event{
		ID:        "evt_123",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_123",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Mentions:  []string{"reviewer"},
			Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: "@reviewer Review this patch"}},
		},
	}

	if err := runEventLoop(context.Background(), config, streamerStub{events: []protocol.Event{event}}, backoffStub{}); err != nil {
		t.Fatalf("runEventLoop() error = %v", err)
	}

	select {
	case payload := <-received:
		if payload.Type != "message.send" {
			t.Fatalf("unexpected inbound type %q", payload.Type)
		}
		if payload.SessionID != "agent:reviewer:local:room:research" {
			t.Fatalf("unexpected inbound session id %q", payload.SessionID)
		}
		content := payload.Payload["content"].(string)
		if !strings.Contains(content, "From: local/agent/writer") {
			t.Fatalf("expected sender metadata, got %q", content)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pico inbound dispatch")
	}

	select {
	case payload := <-received:
		t.Fatalf("expected no bootstrap dispatch for mention-only room, got %#v", payload)
	default:
	}
}
