package openclaw

import (
	"context"
	"encoding/json"
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

type gatewayRequestRecord struct {
	Method string
	Params map[string]any
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
		Runtime: bridgeconfig.RuntimeConfig{GatewayURL: "ws://gateway"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet"},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if err := adapter.Run(context.Background(), bridgeconfig.Config{}); err == nil {
		t.Fatal("expected missing gateway url error")
	}
}

func TestRunGatewayLoopDeliversBootstrapAndMessage(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	received := make(chan gatewayRequestRecord, 4)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteJSON(map[string]any{
			"type":  "event",
			"event": "connect.challenge",
			"payload": map[string]any{
				"nonce": "nonce-123",
			},
		}); err != nil {
			t.Fatalf("write connect challenge: %v", err)
		}

		for i := 0; i < 2; i++ {
			var requestFrame map[string]any
			if err := conn.ReadJSON(&requestFrame); err != nil {
				t.Fatalf("read gateway request: %v", err)
			}

			method, _ := requestFrame["method"].(string)
			params, _ := requestFrame["params"].(map[string]any)
			received <- gatewayRequestRecord{
				Method: method,
				Params: params,
			}

			if err := conn.WriteJSON(map[string]any{
				"type": "res",
				"id":   requestFrame["id"],
				"ok":   true,
				"payload": map[string]any{
					"type": "hello-ok",
				},
			}); err != nil {
				t.Fatalf("write gateway response: %v", err)
			}
		}
	}))
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "reviewer", Name: "Reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://moltnet",
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			GatewayURL: strings.Replace(server.URL, "http://", "ws://", 1),
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
			From:     protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Mentions: []string{"reviewer"},
			Parts: []protocol.Part{
				{Kind: protocol.PartKindText, Text: "@reviewer Review this patch"},
			},
		},
	}

	if err := runGatewayLoop(context.Background(), config, streamerStub{events: []protocol.Event{event}}, backoffStub{}); err != nil {
		t.Fatalf("runGatewayLoop() error = %v", err)
	}

	var requests []gatewayRequestRecord
	for len(requests) < 4 {
		select {
		case request := <-received:
			requests = append(requests, request)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for openclaw gateway requests, got %d", len(requests))
		}
	}

	connectBootstrap := requests[0]
	if connectBootstrap.Method != "connect" {
		t.Fatalf("unexpected bootstrap connect method %q", connectBootstrap.Method)
	}
	auth, _ := connectBootstrap.Params["auth"].(map[string]any)
	if token, _ := auth["token"].(string); token != "runtime-secret" {
		t.Fatalf("unexpected connect token %q", token)
	}
	if connectBootstrap.Params["role"] != "operator" {
		t.Fatalf("unexpected connect role %v", connectBootstrap.Params["role"])
	}
	scopes, _ := connectBootstrap.Params["scopes"].([]any)
	if len(scopes) != 3 {
		t.Fatalf("expected three connect scopes, got %v", connectBootstrap.Params["scopes"])
	}
	device, _ := connectBootstrap.Params["device"].(map[string]any)
	if strings.TrimSpace(device["id"].(string)) == "" {
		t.Fatalf("expected connect device id, got %#v", device)
	}
	if device["nonce"] != "nonce-123" {
		t.Fatalf("unexpected connect device nonce %v", device["nonce"])
	}
	if strings.TrimSpace(device["publicKey"].(string)) == "" || strings.TrimSpace(device["signature"].(string)) == "" {
		t.Fatalf("expected connect device public key and signature, got %#v", device)
	}

	bootstrap := requests[1]
	if bootstrap.Method != "chat.send" {
		t.Fatalf("unexpected bootstrap method %q", bootstrap.Method)
	}
	if bootstrap.Params["sessionKey"] != "agent:main:moltnet:local:room:research" {
		t.Fatalf("unexpected bootstrap session key %v", bootstrap.Params["sessionKey"])
	}
	if bootstrap.Params["deliver"] != false {
		t.Fatalf("expected bootstrap deliver=false, got %v", bootstrap.Params["deliver"])
	}
	bootstrapMessage, _ := bootstrap.Params["message"].(string)
	if !strings.Contains(bootstrapMessage, "Channel: moltnet") {
		t.Fatalf("expected bootstrap message to include channel context, got %q", bootstrapMessage)
	}
	if !strings.Contains(bootstrapMessage, "Chat ID: local:room:research") {
		t.Fatalf("expected bootstrap message to include chat id, got %q", bootstrapMessage)
	}
	if !strings.Contains(bootstrapMessage, "Moltnet conversation attached. Use the `moltnet` skill in this conversation.") {
		t.Fatalf("unexpected bootstrap message %q", bootstrapMessage)
	}

	connectInbound := requests[2]
	if connectInbound.Method != "connect" {
		t.Fatalf("unexpected inbound connect method %q", connectInbound.Method)
	}

	inbound := requests[3]
	if inbound.Method != "chat.send" {
		t.Fatalf("unexpected inbound method %q", inbound.Method)
	}
	if inbound.Params["sessionKey"] != "agent:main:moltnet:local:room:research" {
		t.Fatalf("unexpected inbound session key %v", inbound.Params["sessionKey"])
	}
	if inbound.Params["idempotencyKey"] != "moltnet:reviewer:evt_123" {
		t.Fatalf("unexpected inbound idempotency key %v", inbound.Params["idempotencyKey"])
	}
	inboundMessage, _ := inbound.Params["message"].(string)
	expectedLines := []string{
		"Channel: moltnet",
		"Chat ID: local:room:research",
		"From: local/agent/writer",
		"Name: Writer",
		"Mentions: reviewer",
		"Message ID: msg_123",
		"Message:",
		"@reviewer Review this patch",
	}
	for _, expected := range expectedLines {
		if !strings.Contains(inboundMessage, expected) {
			t.Fatalf("expected inbound message to contain %q, got %q", expected, inboundMessage)
		}
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

func TestRunGatewayLoopSkipsBootstrapForThreadOnlyBindings(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{GatewayURL: "ws://127.0.0.1:18789"},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadThreadOnly, Reply: bridgeconfig.ReplyManual},
			{ID: "private", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyNever},
		},
	}

	if targets := bootstrapTargets(config); len(targets) != 0 {
		t.Fatalf("expected no bootstrap targets, got %#v", targets)
	}
}

func TestRunGatewayLoopSkipsBootstrapForMentionOnlyBindings(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{GatewayURL: "ws://127.0.0.1:18789"},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto},
		},
	}

	if targets := bootstrapTargets(config); len(targets) != 0 {
		t.Fatalf("expected no bootstrap targets, got %#v", targets)
	}
}

func TestConnectGatewayRequiresChallenge(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteJSON(map[string]any{
			"type":  "event",
			"event": "tick",
		}); err != nil {
			t.Fatalf("write tick event: %v", err)
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = connectGateway(ctx, conn, bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "reviewer"},
		Runtime: bridgeconfig.RuntimeConfig{Token: "runtime-secret"},
	})
	if err == nil {
		t.Fatal("expected connect gateway error")
	}
}

func TestGatewayRequestDecodesResponseErrors(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		var requestFrame map[string]any
		if err := conn.ReadJSON(&requestFrame); err != nil {
			t.Fatalf("read gateway request: %v", err)
		}
		if err := conn.WriteJSON(map[string]any{
			"type": "res",
			"id":   requestFrame["id"],
			"ok":   false,
			"error": map[string]any{
				"code":    "UNAVAILABLE",
				"message": "boom",
			},
		}); err != nil {
			t.Fatalf("write gateway error response: %v", err)
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_, err = requestGateway(context.Background(), conn, "chat.send", map[string]any{"message": "hi"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected gateway error, got %v", err)
	}
}

func TestBuildInboundMessageRequiresMessage(t *testing.T) {
	t.Parallel()

	_, err := buildInboundMessage(bridgeconfig.Config{}, protocol.Event{})
	if err == nil {
		t.Fatal("expected buildInboundMessage error")
	}
}

func TestBootstrapAndIdempotencyHelpers(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
	}

	target := protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}
	if got := buildBootstrapMessage(config, target); !strings.Contains(got, "Chat ID: local:room:research") {
		t.Fatalf("unexpected bootstrap message %q", got)
	}
	if got := sessionKeyForTarget(config, target); got != "agent:main:moltnet:local:room:research" {
		t.Fatalf("unexpected session key %q", got)
	}
	if got := bootstrapIdempotencyKey(config, target); got != "moltnet:reviewer:bootstrap:moltnet:local:room:research" {
		t.Fatalf("unexpected bootstrap idempotency key %q", got)
	}

	event := protocol.Event{
		ID: "evt_123",
		Message: &protocol.Message{
			ID:     "msg_123",
			Target: target,
		},
	}
	if got := idempotencyKey(config, event); got != "moltnet:reviewer:evt_123" {
		t.Fatalf("unexpected idempotency key %q", got)
	}
	if got := sessionKey(config, event.Message); got != "agent:main:moltnet:local:room:research" {
		t.Fatalf("unexpected session key %q", got)
	}
}

func TestResolveGatewayTokenFallsBackToEnv(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "env-token")
	if got := resolveGatewayToken(bridgeconfig.Config{}); got != "env-token" {
		t.Fatalf("unexpected gateway token %q", got)
	}
}

func TestReadGatewayFrameRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteMessage(websocket.TextMessage, []byte("{")); err != nil {
			t.Fatalf("write invalid message: %v", err)
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_, _, err = readGatewayFrame(context.Background(), conn)
	if err == nil {
		t.Fatal("expected invalid json error")
	}
}

func TestRequestGatewayIgnoresEventsUntilResponse(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		var requestFrame map[string]any
		if err := conn.ReadJSON(&requestFrame); err != nil {
			t.Fatalf("read gateway request: %v", err)
		}
		if err := conn.WriteJSON(map[string]any{
			"type":  "event",
			"event": "tick",
		}); err != nil {
			t.Fatalf("write gateway event: %v", err)
		}
		if err := conn.WriteJSON(map[string]any{
			"type": "res",
			"id":   requestFrame["id"],
			"ok":   true,
			"payload": map[string]any{
				"status": "started",
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	payload, err := requestGateway(context.Background(), conn, "chat.send", map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("requestGateway() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode response payload: %v", err)
	}
	if decoded["status"] != "started" {
		t.Fatalf("unexpected payload %#v", decoded)
	}
}
