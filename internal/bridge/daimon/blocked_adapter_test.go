package daimon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAdapterDefersWithoutACKThenRecoversSameDelivery(t *testing.T) {
	t.Setenv("DAIMON_ADAPTER_TOKEN", "test-bearer")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	delivery := daimonDelivery()
	delivery.Message = "[room research] writer\nhello"
	event := protocol.Event{
		ID: "cursor_pending", Type: protocol.EventTypeMessageCreated, NetworkID: "local",
		Message: &protocol.Message{
			ID: "msg_1", NetworkID: "local", Target: delivery.Target,
			From: protocol.Actor{Type: "agent", ID: "writer"}, Mentions: []string{"researcher"},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}}, CreatedAt: delivery.OccurredAt,
		}, CreatedAt: delivery.OccurredAt,
	}
	config := daimonConfig("http://control.invalid")
	var mu sync.Mutex
	var requestBodies []string
	var requestTimes []time.Time
	var attachments, failures, publications int
	acked := make(chan struct{})
	published := make(chan struct{})
	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/wakes":
			body, _ := io.ReadAll(request.Body)
			mu.Lock()
			requestBodies = append(requestBodies, string(body))
			requestTimes = append(requestTimes, time.Now())
			attempt := len(requestBodies)
			mu.Unlock()
			if attempt <= 2 {
				response.WriteHeader(http.StatusConflict)
				_, _ = response.Write([]byte(blockedBody("operator_stop", 1000)))
				return
			}
			response.WriteHeader(http.StatusAccepted)
			_, _ = response.Write([]byte(acceptanceBody(t, config, delivery)))
		case "/v2/wake-receipts/11111111-1111-4111-8111-111111111111":
			select {
			case <-acked:
			case <-request.Context().Done():
				return
			}
			_, _ = response.Write([]byte(receiptBody(t, config, delivery, "completed", "recovered reply")))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer controlServer.Close()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	moltnetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			mu.Lock()
			attachments++
			attempt := attachments
			mu.Unlock()
			conn, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			identify := blockedHandshake(t, conn)
			if attempt == 1 {
				// A skipped, unrelated event establishes the prior ACK cursor.
				prior := protocol.Event{ID: "cursor_saved", Type: "unrelated", NetworkID: "local"}
				_ = conn.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpEvent, Version: protocol.AttachmentProtocolV1, Cursor: prior.ID, Event: &prior})
				var ack protocol.AttachmentFrame
				if err := conn.ReadJSON(&ack); err != nil || ack.Op != protocol.AttachmentOpAck || ack.Cursor != prior.ID {
					t.Errorf("prior cursor not ACKed: %#v, %v", ack, err)
					return
				}
			} else if identify.Cursor != "cursor_saved" {
				t.Errorf("deferred delivery advanced cursor: %q", identify.Cursor)
			}
			_ = conn.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpEvent, Version: protocol.AttachmentProtocolV1, Cursor: event.ID, Event: &event})
			var frame protocol.AttachmentFrame
			err = conn.ReadJSON(&frame)
			if attempt <= 2 {
				if err == nil {
					t.Errorf("blocked delivery sent ACK/error frame: %#v", frame)
				}
				return
			}
			if err != nil || frame.Op != protocol.AttachmentOpAck || frame.Cursor != event.ID {
				t.Errorf("accepted delivery failed ACK: %#v, %v", frame, err)
				return
			}
			close(acked)
			select {
			case <-published:
			case <-ctx.Done():
				t.Error("recovered receipt did not publish")
				return
			}
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
		case "/v1/messages":
			var payload protocol.SendMessageRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			publications++
			mu.Unlock()
			if len(payload.Parts) != 1 || payload.Parts[0].Text != "recovered reply" {
				t.Errorf("unexpected recovered reply: %#v", payload.Parts)
			}
			_, _ = fmt.Fprintf(response, `{"message_id":%q,"event_id":"evt_reply","accepted":true}`, payload.ID)
			close(published)
		case "/v1/agents/wake-failed":
			mu.Lock()
			failures++
			mu.Unlock()
			response.WriteHeader(http.StatusAccepted)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer moltnetServer.Close()
	runConfig := config
	runConfig.Moltnet.BaseURL = moltnetServer.URL
	runConfig.Runtime.ControlURL = controlServer.URL
	runConfig.Runtime.TokenEnv = "DAIMON_ADAPTER_TOKEN"
	runConfig.Runtime.ReceiptStorePath = filepath.Join(t.TempDir(), "private", "receipts.json")
	runConfig.Rooms = []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeMentions}}
	if err := New().Run(ctx, runConfig); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requestBodies) != 3 || attachments != 3 || failures != 0 || publications != 1 {
		t.Fatalf("requests=%d attachments=%d failures=%d publications=%d, want 3/3/0/1", len(requestBodies), attachments, failures, publications)
	}
	for i := 1; i < len(requestBodies); i++ {
		if requestBodies[i] != requestBodies[0] {
			t.Fatal("retry changed delivery identity or wake content")
		}
		if requestTimes[i].Sub(requestTimes[i-1]) < time.Second {
			t.Fatal("retried before the runtime's deferred interval")
		}
	}
}

func blockedHandshake(t *testing.T, conn *websocket.Conn) protocol.AttachmentFrame {
	t.Helper()
	_ = conn.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpHello, Version: protocol.AttachmentProtocolV1, HeartbeatIntervalMS: 30000})
	var identify protocol.AttachmentFrame
	if err := conn.ReadJSON(&identify); err != nil {
		t.Error(err)
	}
	_ = conn.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpReady, Version: protocol.AttachmentProtocolV1, NetworkID: "local", AgentID: "researcher"})
	return identify
}
