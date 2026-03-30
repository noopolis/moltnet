package tinyclaw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func validBridgeConfig(baseURL string) bridgeconfig.Config {
	return bridgeconfig.Config{
		Version: bridgeconfig.VersionV1,
		Agent: bridgeconfig.AgentConfig{
			ID:   "researcher",
			Name: "Researcher",
		},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   baseURL,
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:        bridgeconfig.RuntimeTinyClaw,
			InboundURL:  baseURL + "/api/message",
			OutboundURL: baseURL + "/api/responses/pending",
			AckURL:      baseURL + "/api/responses",
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadAll},
		},
	}
}

func TestNewBridgeAndAdapterRun(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	config := validBridgeConfig(server.URL)
	config.Runtime.Channel = ""
	config.Agent.Name = ""

	bridge, err := newBridge(config)
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}
	if bridge.channel != defaultChannelName || bridge.agentName != "researcher" {
		t.Fatalf("unexpected bridge %#v", bridge)
	}

	adapter := New()
	if adapter.Name() != bridgeconfig.RuntimeTinyClaw {
		t.Fatalf("unexpected adapter name %q", adapter.Name())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Run(ctx, config); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestNewBridgeValidation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	config := validBridgeConfig(server.URL)

	config.Runtime.InboundURL = ""
	if _, err := newBridge(config); err == nil {
		t.Fatal("expected inbound url error")
	}

	config = validBridgeConfig(server.URL)
	config.Runtime.OutboundURL = ""
	if _, err := newBridge(config); err == nil {
		t.Fatal("expected outbound url error")
	}

	config = validBridgeConfig(server.URL)
	config.Runtime.AckURL = ""
	if _, err := newBridge(config); err == nil {
		t.Fatal("expected ack url error")
	}
}

func TestFlushResponses(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var acked []string
	var sentBodies []string

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/responses/pending":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{"id":7,"sender":"Writer","senderId":"room:research","message":"done","files":["report.md"]}]`))
		case "/api/responses/7/ack":
			mu.Lock()
			acked = append(acked, request.URL.Path)
			mu.Unlock()
			response.WriteHeader(http.StatusOK)
		case "/v1/messages":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatalf("decode send body: %v", err)
			}
			bytes, _ := json.Marshal(body)
			mu.Lock()
			sentBodies = append(sentBodies, string(bytes))
			mu.Unlock()
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"message_id":"msg_1","event_id":"evt_1","accepted":true}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	bridge, err := newBridge(validBridgeConfig(server.URL))
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}

	if err := bridge.flushResponses(context.Background()); err != nil {
		t.Fatalf("flushResponses() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sentBodies) != 1 || !strings.Contains(sentBodies[0], "report.md") {
		t.Fatalf("unexpected sent bodies %#v", sentBodies)
	}
	if len(acked) != 1 || acked[0] != "/api/responses/7/ack" {
		t.Fatalf("unexpected acked paths %#v", acked)
	}
}

func TestRunInboundSkipsUnrelatedMessage(t *testing.T) {
	t.Parallel()

	var posted int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/events/stream":
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = response.Write([]byte("event: message.created\n"))
			_, _ = response.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"message.created\",\"network_id\":\"local\",\"message\":{\"id\":\"msg_1\",\"network_id\":\"local\",\"target\":{\"kind\":\"room\",\"room_id\":\"unknown\"},\"from\":{\"type\":\"agent\",\"id\":\"writer\"},\"parts\":[{\"kind\":\"text\",\"text\":\"hello\"}],\"created_at\":\"2026-03-30T12:00:00Z\"}}\n\n"))
		case "/api/message":
			posted++
			response.WriteHeader(http.StatusOK)
		default:
			response.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	bridge, err := newBridge(validBridgeConfig(server.URL))
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}

	if err := bridge.runInbound(context.Background()); err != nil {
		t.Fatalf("runInbound() error = %v", err)
	}
	if posted != 0 {
		t.Fatalf("expected no posted messages, got %d", posted)
	}
}

func TestRunInboundPostsRelevantMessage(t *testing.T) {
	t.Parallel()

	var posted tinyclawMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/events/stream":
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = response.Write([]byte("event: message.created\n"))
			_, _ = response.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"message.created\",\"network_id\":\"local\",\"message\":{\"id\":\"msg_1\",\"network_id\":\"local\",\"target\":{\"kind\":\"room\",\"room_id\":\"research\"},\"from\":{\"type\":\"agent\",\"id\":\"writer\",\"name\":\"Writer\"},\"mentions\":[\"researcher\"],\"parts\":[{\"kind\":\"text\",\"text\":\"hello\"}],\"created_at\":\"2026-03-30T12:00:00Z\"}}\n\n"))
		case "/api/message":
			if err := json.NewDecoder(request.Body).Decode(&posted); err != nil {
				t.Fatalf("decode posted message: %v", err)
			}
			response.WriteHeader(http.StatusOK)
		default:
			response.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	bridge, err := newBridge(validBridgeConfig(server.URL))
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}

	if err := bridge.runInbound(context.Background()); err != nil {
		t.Fatalf("runInbound() error = %v", err)
	}
	if posted.Agent != "researcher" || posted.SenderID != "room:research" {
		t.Fatalf("unexpected posted message %#v", posted)
	}
}

func TestRunOutboundStopsOnCancel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/responses/pending" {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[]`))
			return
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	bridge, err := newBridge(validBridgeConfig(server.URL))
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	if err := bridge.runOutbound(ctx); err != nil {
		t.Fatalf("runOutbound() error = %v", err)
	}
}

func TestFlushResponsesErrorPaths(t *testing.T) {
	t.Parallel()

	pendingFailure := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusBadGateway)
	}))
	defer pendingFailure.Close()

	bridge, err := newBridge(validBridgeConfig(pendingFailure.URL))
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}
	if err := bridge.flushResponses(context.Background()); err == nil {
		t.Fatal("expected pending response error")
	}

	sendFailure := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/responses/pending":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{"id":7,"sender":"Writer","senderId":"room:research","message":"done"}]`))
		case "/v1/messages":
			response.WriteHeader(http.StatusBadGateway)
		default:
			response.WriteHeader(http.StatusOK)
		}
	}))
	defer sendFailure.Close()

	bridge, err = newBridge(validBridgeConfig(sendFailure.URL))
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}
	if err := bridge.flushResponses(context.Background()); err == nil {
		t.Fatal("expected moltnet send error")
	}

	skipInvalid := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/responses/pending":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{"id":7,"sender":"Writer","senderId":"bad","message":"done"}]`))
		default:
			response.WriteHeader(http.StatusOK)
		}
	}))
	defer skipInvalid.Close()

	bridge, err = newBridge(validBridgeConfig(skipInvalid.URL))
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}
	if err := bridge.flushResponses(context.Background()); err != nil {
		t.Fatalf("expected invalid response to be skipped, got %v", err)
	}
}

func TestAdapterRunReturnsConfigError(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeTinyClaw},
	}
	adapter := New()

	if err := adapter.Run(context.Background(), config); err == nil {
		t.Fatal("expected config error")
	}
}
