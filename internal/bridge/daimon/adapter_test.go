package daimon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAdapterACKsAcceptanceBeforeAsyncResultAndPublishesOnce(t *testing.T) {
	const token = "daimon-adapter-bearer"
	t.Setenv("DAIMON_ADAPTER_TOKEN", token)

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
			Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
			CreatedAt: time.Date(2026, 8, 24, 12, 0, 0, 123000000, time.UTC),
		},
		CreatedAt: time.Date(2026, 8, 24, 12, 0, 0, 123000000, time.UTC),
	}

	var mu sync.Mutex
	var wakeRequests int
	var published []protocol.SendMessageRequest
	acked := make(chan struct{})
	publishedResult := make(chan struct{})
	delivery := loop.ControlDelivery{
		Target:     event.Message.Target,
		EventID:    protocol.MessageEventID(event.Message.ID),
		Message:    "[room research] Writer\nhello",
		OccurredAt: event.Message.CreatedAt,
	}
	config := daimonConfig("http://control.invalid")
	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("unexpected Daimon request %s %s auth=%q", request.Method, request.URL.Path, request.Header.Get("Authorization"))
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		if request.Method == http.MethodGet && request.URL.Path == "/v2/wake-receipts/11111111-1111-4111-8111-111111111111" {
			select {
			case <-acked:
			case <-request.Context().Done():
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(receiptBody(t, config, delivery, "completed", "finished research")))
			return
		}
		if request.Method != http.MethodPost || request.URL.Path != "/v2/wakes" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		var payload wakeRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode Daimon wake: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.AgentID != "agent:researcher" || payload.DeliveryID != "moltnet:msg_1" || payload.Event.Kind != "message" || payload.Event.OccurredAt != "2026-08-24T12:00:00.123Z" {
			t.Errorf("unexpected Daimon wake %#v", payload)
		}
		mu.Lock()
		wakeRequests++
		mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusAccepted)
		_, _ = response.Write([]byte(acceptanceBody(t, config, delivery)))
	}))
	t.Cleanup(controlServer.Close)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	moltnetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			connection, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				t.Errorf("upgrade attachment: %v", err)
				return
			}
			defer connection.Close()
			writeDaimonHandshake(t, connection)
			for replay := 0; replay < 2; replay++ {
				if err := connection.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpEvent, Version: protocol.AttachmentProtocolV1, NetworkID: "local", Cursor: event.ID, Event: &event}); err != nil {
					t.Errorf("write attachment event: %v", err)
					return
				}
				var ack protocol.AttachmentFrame
				if err := connection.ReadJSON(&ack); err != nil || ack.Op != protocol.AttachmentOpAck || ack.Cursor != event.ID {
					t.Errorf("unexpected attachment ack %#v, %v", ack, err)
					return
				}
				if replay == 0 {
					close(acked)
				}
			}
			select {
			case <-publishedResult:
			case <-time.After(5 * time.Second):
				t.Error("timed out waiting for async Daimon result publication")
				return
			}
			_ = connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
		case "/v1/messages":
			var payload protocol.SendMessageRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Errorf("decode publication: %v", err)
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			published = append(published, payload)
			mu.Unlock()
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(fmt.Sprintf(`{"message_id":%q,"event_id":"evt_2","accepted":true}`, payload.ID)))
			select {
			case <-publishedResult:
			default:
				close(publishedResult)
			}
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(moltnetServer.Close)

	err := New().Run(context.Background(), bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: moltnetServer.URL, NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeDaimon, AgentID: "agent:researcher", ControlURL: controlServer.URL, TokenEnv: "DAIMON_ADAPTER_TOKEN", ReceiptStorePath: filepath.Join(t.TempDir(), "private", "receipts.json")},
		Rooms:   []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeMentions}},
	})
	if err != nil {
		t.Fatalf("Adapter.Run() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if wakeRequests != 2 {
		t.Fatalf("wake requests = %d, want replay delivered twice with one deterministic wake id", wakeRequests)
	}
	if len(published) != 1 || published[0].ID != bridgeutil.DeliveryReplyMessageID("researcher", "moltnet:msg_1", published[0].Target) || published[0].From.ID != "researcher" || published[0].Target.RoomID != "research" || published[0].Parts[0].Text != "finished research" {
		t.Fatalf("unexpected durable async publication: %#v", published)
	}
	if len(published[0].CauseEventIDs) != 1 || published[0].CauseEventIDs[0] != "daimon:moltnet:msg_1:turn.output.completed" {
		t.Fatalf("unexpected async publication causes: %#v", published[0].CauseEventIDs)
	}
}

func TestAdapterResolvesTokenOnlyWhenRun(t *testing.T) {
	adapter := New()
	if adapter.Name() != bridgeconfig.RuntimeDaimon {
		t.Fatalf("Name() = %q", adapter.Name())
	}
	config := daimonConfig("http://127.0.0.1:1")
	config.Runtime.ReceiptStorePath = filepath.Join(t.TempDir(), "private", "receipts.json")
	config.Runtime.TokenEnv = "MISSING_DAIMON_ADAPTER_TOKEN"
	if err := adapter.Run(context.Background(), config); err == nil || !strings.Contains(err.Error(), config.Runtime.TokenEnv) {
		t.Fatalf("expected missing runtime token error, got %v", err)
	}
	config.Runtime.ControlURL = ""
	if err := adapter.Run(context.Background(), config); err == nil || !strings.Contains(err.Error(), "control_url") {
		t.Fatalf("expected missing control url error, got %v", err)
	}

	t.Setenv("DAIMON_CANCELLED_TOKEN", "cancelled-bearer-canary")
	config.Runtime.ControlURL = "http://127.0.0.1:1"
	config.Runtime.TokenEnv = "DAIMON_CANCELLED_TOKEN"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Run(ctx, config); err != nil {
		t.Fatalf("cancelled Adapter.Run() error = %v", err)
	}
}

func writeDaimonHandshake(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	if err := connection.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpHello, Version: protocol.AttachmentProtocolV1, HeartbeatIntervalMS: 30000}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var identify protocol.AttachmentFrame
	if err := connection.ReadJSON(&identify); err != nil || identify.Op != protocol.AttachmentOpIdentify || identify.Agent == nil || identify.Agent.ID != "researcher" {
		t.Fatalf("unexpected identify %#v, %v", identify, err)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpReady, Version: protocol.AttachmentProtocolV1, NetworkID: "local", AgentID: "researcher"}); err != nil {
		t.Fatalf("write ready: %v", err)
	}
}

func receiptBody(t *testing.T, config bridgeconfig.Config, delivery loop.ControlDelivery, state, text string) string {
	t.Helper()
	digest, err := wakeDigest(config, delivery)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"version":        wakeReceiptVersion,
		"acceptance_id":  "11111111-1111-4111-8111-111111111111",
		"agent_id":       runtimeAgentID(config),
		"delivery_id":    delivery.EventID,
		"request_digest": digest,
		"state":          state,
		"accepted_at":    "2026-08-24T08:11:12.345Z",
		"updated_at":     "2026-08-24T08:12:12.345Z",
	}
	if text != "" || state == "completed" {
		payload["text"] = text
	}
	contents, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
