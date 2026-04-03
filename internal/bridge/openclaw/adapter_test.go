package openclaw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestAdapter(t *testing.T) {
	t.Parallel()

	adapter := New()
	if adapter.Name() != bridgeconfig.RuntimeOpenClaw {
		t.Fatalf("unexpected name %q", adapter.Name())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := adapter.Run(ctx, bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: "http://control"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet"},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if err := adapter.Run(context.Background(), bridgeconfig.Config{}); err == nil {
		t.Fatal("expected missing control url error")
	}
}

func TestRunHookLoopDeliversManualRoomMessage(t *testing.T) {
	t.Parallel()

	type receivedHook struct {
		Message        string `json:"message"`
		Name           string `json:"name"`
		AgentID        string `json:"agentId"`
		SessionKey     string `json:"sessionKey"`
		IdempotencyKey string `json:"idempotencyKey"`
		DisableMessage bool   `json:"disableMessageTool"`
		Deliver        bool   `json:"deliver"`
	}
	var received []receivedHook
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if auth := request.Header.Get("Authorization"); auth != "Bearer runtime-secret" {
			t.Fatalf("unexpected auth header %q", auth)
		}
		defer request.Body.Close()
		var payload receivedHook
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		received = append(received, payload)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "reviewer", Name: "Reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://moltnet",
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			ControlURL: server.URL,
			Token:      "runtime-secret",
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyManual},
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

	if err := runHookLoop(context.Background(), config, streamerStub{events: []protocol.Event{event}}, server.Client(), backoffStub{}); err != nil {
		t.Fatalf("runHookLoop() error = %v", err)
	}

	if len(received) != 2 {
		t.Fatalf("expected bootstrap + message hook, got %d requests", len(received))
	}

	bootstrap := received[0]
	if bootstrap.Name != "Moltnet Bootstrap" {
		t.Fatalf("unexpected bootstrap hook name %q", bootstrap.Name)
	}
	if bootstrap.SessionKey != "hook:moltnet:local:room:research" {
		t.Fatalf("unexpected bootstrap session key %q", bootstrap.SessionKey)
	}
	if bootstrap.IdempotencyKey != "moltnet:reviewer:bootstrap:moltnet:local:room:research" {
		t.Fatalf("unexpected bootstrap idempotency key %q", bootstrap.IdempotencyKey)
	}
	if !strings.Contains(bootstrap.Message, `"kind": "bootstrap"`) {
		t.Fatalf("expected bootstrap prompt kind, got %q", bootstrap.Message)
	}

	message := received[1]
	if message.AgentID != "reviewer" {
		t.Fatalf("unexpected agent id %q", message.AgentID)
	}
	if message.Name != "Moltnet" {
		t.Fatalf("unexpected hook name %q", message.Name)
	}
	if message.Deliver {
		t.Fatal("expected deliver=false for hook dispatch")
	}
	if message.SessionKey != "hook:moltnet:local:room:research" {
		t.Fatalf("unexpected session key %q", message.SessionKey)
	}
	if message.IdempotencyKey != "moltnet:reviewer:evt_123" {
		t.Fatalf("unexpected idempotency key %q", message.IdempotencyKey)
	}
	if !message.DisableMessage {
		t.Fatal("expected disableMessageTool=true for Moltnet hook dispatch")
	}
	if !strings.Contains(message.Message, `"kind": "room"`) {
		t.Fatalf("expected room target in hook message, got %q", message.Message)
	}
	if !strings.Contains(message.Message, `"room_id": "research"`) {
		t.Fatalf("expected room id in hook message, got %q", message.Message)
	}
	if !strings.Contains(message.Message, `Do not answer this delivery with a status summary.`) {
		t.Fatalf("expected action-oriented delivery guidance in hook message, got %q", message.Message)
	}
	if !strings.Contains(message.Message, `If your own instructions say to coordinate privately`) {
		t.Fatalf("expected hook message to respect local coordination instructions, got %q", message.Message)
	}
	if !strings.Contains(message.Message, `moltnet send --target`) {
		t.Fatalf("expected CLI send guidance in hook message, got %q", message.Message)
	}
	if strings.Contains(message.Message, `skills/moltnet/SKILL.md`) {
		t.Fatalf("expected hook message to avoid direct skill path guidance, got %q", message.Message)
	}
}

func TestShouldDeliverIgnoresReplyNeverAndThreads(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{
			NetworkID: "local",
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyNever},
		},
	}

	roomEvent := protocol.Event{
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{ID: "writer"},
		},
	}
	if shouldDeliver(config, roomEvent) {
		t.Fatal("expected reply=never room event to be ignored")
	}

	threadEvent := protocol.Event{
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			NetworkID: "local",
			Target: protocol.Target{
				Kind:     protocol.TargetKindThread,
				RoomID:   "research",
				ThreadID: "thread_1",
			},
			From: protocol.Actor{ID: "writer"},
		},
	}
	if shouldDeliver(config, threadEvent) {
		t.Fatal("expected thread event to be ignored")
	}
}

func TestRunHookLoopSkipsBootstrapForThreadOnlyBindings(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: server.URL},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadThreadOnly, Reply: bridgeconfig.ReplyManual},
			{ID: "private", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyNever},
		},
	}

	if err := runHookLoop(context.Background(), config, streamerStub{}, server.Client(), backoffStub{}); err != nil {
		t.Fatalf("runHookLoop() error = %v", err)
	}

	if requests != 0 {
		t.Fatalf("expected no bootstrap hooks, got %d", requests)
	}
}
