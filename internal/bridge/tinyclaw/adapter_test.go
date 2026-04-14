package tinyclaw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
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

	controlConfig := config
	controlConfig.Runtime.ControlURL = server.URL + "/control"
	if err := adapter.Run(ctx, controlConfig); err != nil {
		t.Fatalf("Run() control path error = %v", err)
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
	var sent int

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
			mu.Lock()
			sent++
			mu.Unlock()
			response.WriteHeader(http.StatusBadGateway)
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
	if sent != 0 {
		t.Fatalf("expected no Moltnet messages, got %d", sent)
	}
	if len(acked) != 1 || acked[0] != "/api/responses/7/ack" {
		t.Fatalf("unexpected acked paths %#v", acked)
	}
}

func TestFlushResponsesLimitsOnePollBatch(t *testing.T) {
	t.Parallel()

	var acked int
	var sent int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/responses/pending":
			response.Header().Set("Content-Type", "application/json")
			payload := make([]string, 0, maxResponsesPerPoll+5)
			for index := 1; index <= maxResponsesPerPoll+5; index++ {
				payload = append(payload, `{"id":`+strconv.Itoa(index)+`,"sender":"Writer","senderId":"room:research","message":"done"}`)
			}
			_, _ = response.Write([]byte("[" + strings.Join(payload, ",") + "]"))
		case "/api/responses/1/ack", "/api/responses/2/ack", "/api/responses/3/ack", "/api/responses/4/ack", "/api/responses/5/ack",
			"/api/responses/6/ack", "/api/responses/7/ack", "/api/responses/8/ack", "/api/responses/9/ack", "/api/responses/10/ack",
			"/api/responses/11/ack", "/api/responses/12/ack", "/api/responses/13/ack", "/api/responses/14/ack", "/api/responses/15/ack",
			"/api/responses/16/ack", "/api/responses/17/ack", "/api/responses/18/ack", "/api/responses/19/ack", "/api/responses/20/ack":
			acked++
			response.WriteHeader(http.StatusOK)
		case "/v1/messages":
			sent++
			response.WriteHeader(http.StatusBadGateway)
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
	if acked != maxResponsesPerPoll {
		t.Fatalf("expected %d acks, got %d", maxResponsesPerPoll, acked)
	}
	if sent != 0 {
		t.Fatalf("expected no Moltnet sends, got %d", sent)
	}
}

func TestFlushResponsesDrainsTinyClawOutputQueue(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var acked []string
	var sent int

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/responses/pending":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[
				{"id":7,"sender":"Director","senderId":"room:research","message":"[tool: Bash]\n\n- [Researcher]"},
				{"id":8,"sender":"Director","senderId":"room:research","message":"My cue! Drafting line: \"hello\"\n\n- [Researcher]"},
				{"id":9,"sender":"Director","senderId":"room:research","message":"Line sent.\n\n- [Researcher]"},
				{"id":10,"sender":"Director","senderId":"room:research","message":"Queue confirmed. Sending.\n\n- [Researcher]"},
				{"id":11,"sender":"Director","senderId":"room:research","message":"Latest director queue is [QUEUE other room:research] \u2014 superseded. I stay silent.\n\n- [Researcher]"},
				{"id":12,"sender":"Director","senderId":"room:research","message":"Natural line here.\n\n- [Researcher]"}
			]`))
		case "/api/responses/7/ack", "/api/responses/8/ack", "/api/responses/9/ack", "/api/responses/10/ack", "/api/responses/11/ack", "/api/responses/12/ack":
			mu.Lock()
			acked = append(acked, request.URL.Path)
			mu.Unlock()
			response.WriteHeader(http.StatusOK)
		case "/v1/messages":
			mu.Lock()
			sent++
			mu.Unlock()
			response.WriteHeader(http.StatusBadGateway)
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
	if sent != 0 {
		t.Fatalf("expected TinyClaw pending responses to stay off Moltnet, got %d sends", sent)
	}
	if len(acked) != 6 {
		t.Fatalf("expected all responses to be acked, got %#v", acked)
	}
}

func TestRunInboundSkipsUnrelatedMessage(t *testing.T) {
	t.Parallel()

	var posted int
	event := protocol.Event{
		ID:        "evt_1",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "unknown"},
			From:      protocol.Actor{Type: "agent", ID: "writer"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
			CreatedAt: time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			serveAttachmentEvent(t, response, request, event)
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
	event := protocol.Event{
		ID:        "evt_1",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Mentions:  []string{"researcher"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
			CreatedAt: time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			serveAttachmentEvent(t, response, request, event)
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

func TestRunInboundReconnectsAfterAttachFailure(t *testing.T) {
	t.Parallel()

	var attachRequests int
	var posted tinyclawMessageRequest
	event := protocol.Event{
		ID:        "evt_1",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Mentions:  []string{"researcher"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
			CreatedAt: time.Now().UTC(),
		},
		CreatedAt: time.Now().UTC(),
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			attachRequests++
			if attachRequests == 1 {
				response.WriteHeader(http.StatusBadGateway)
				return
			}
			serveAttachmentEvent(t, response, request, event)
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
	if attachRequests < 2 {
		t.Fatalf("expected reconnect after attach failure, got %d requests", attachRequests)
	}
	if posted.Agent != "researcher" {
		t.Fatalf("expected reconnected message delivery, got %#v", posted)
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

	ackFailure := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/responses/pending":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{"id":7,"sender":"Writer","senderId":"room:research","message":"done"}]`))
		case "/api/responses/7/ack":
			response.WriteHeader(http.StatusBadGateway)
		default:
			response.WriteHeader(http.StatusOK)
		}
	}))
	defer ackFailure.Close()

	bridge, err = newBridge(validBridgeConfig(ackFailure.URL))
	if err != nil {
		t.Fatalf("newBridge() error = %v", err)
	}
	if err := bridge.flushResponses(context.Background()); err == nil {
		t.Fatal("expected ack error")
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

func serveAttachmentEvent(
	t *testing.T,
	response http.ResponseWriter,
	request *http.Request,
	event protocol.Event,
) {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	connection, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		t.Fatalf("upgrade websocket: %v", err)
	}
	defer connection.Close()

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:                  protocol.AttachmentOpHello,
		Version:             protocol.AttachmentProtocolV1,
		HeartbeatIntervalMS: 30000,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	var identify protocol.AttachmentFrame
	if err := connection.ReadJSON(&identify); err != nil {
		t.Fatalf("read identify: %v", err)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpReady,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		AgentID:   "researcher",
	}); err != nil {
		t.Fatalf("write ready: %v", err)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpEvent,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Cursor:    event.ID,
		Event:     &event,
	}); err != nil {
		t.Fatalf("write event: %v", err)
	}

	var ack protocol.AttachmentFrame
	if err := connection.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}

	_ = connection.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
		time.Now().Add(time.Second),
	)
}
