package loop

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunControlLoopStartsStreamBeforeBootstrap(t *testing.T) {
	t.Parallel()

	bootstrapStarted := make(chan struct{})
	eventHandled := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	moltnetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
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

			select {
			case <-bootstrapStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("bootstrap control request never started")
			}

			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpEvent,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: "local",
				Cursor:    "evt_1",
				Event: &protocol.Event{
					ID:        "evt_1",
					Type:      protocol.EventTypeMessageCreated,
					NetworkID: "local",
					Message: &protocol.Message{
						ID:        "msg_1",
						NetworkID: "local",
						Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
						From:      protocol.Actor{Type: "agent", ID: "writer"},
						Mentions:  []string{"researcher"},
						Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
						CreatedAt: time.Now().UTC(),
					},
					CreatedAt: time.Now().UTC(),
				},
			}); err != nil {
				t.Fatalf("write event: %v", err)
			}

			select {
			case <-eventHandled:
			case <-time.After(2 * time.Second):
				t.Fatal("event was not handled while bootstrap was in flight")
			}

			var ack protocol.AttachmentFrame
			if err := connection.ReadJSON(&ack); err != nil {
				t.Fatalf("read ack: %v", err)
			}
			if ack.Op != protocol.AttachmentOpAck || ack.Cursor != "evt_1" {
				t.Fatalf("unexpected ack %#v", ack)
			}

			_ = connection.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
				time.Now().Add(time.Second),
			)
		case "/v1/messages":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"message_id":"msg_2","event_id":"evt_2","accepted":true}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer moltnetServer.Close()

	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		defer request.Body.Close()
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read control body: %v", err)
		}
		payload := string(body)

		response.Header().Set("Content-Type", "application/json")
		if strings.Contains(payload, `"from":"Moltnet Bootstrap"`) {
			close(bootstrapStarted)
			select {
			case <-eventHandled:
			case <-time.After(2 * time.Second):
				t.Fatal("bootstrap returned before live event was handled")
			}
			_, _ = response.Write([]byte(`{"from":"researcher","message":""}`))
			return
		}

		close(eventHandled)
		_, _ = response.Write([]byte(`{"from":"researcher","message":"done"}`))
	}))
	defer controlServer.Close()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: moltnetServer.URL, NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: controlServer.URL},
		Rooms:   []bridgeconfig.RoomBinding{{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto}},
	}

	if err := RunControlLoop(context.Background(), config); err != nil {
		t.Fatalf("RunControlLoop() error = %v", err)
	}
}
