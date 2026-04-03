package tinyclaw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMoltnetClientUsesNativeAttachmentGateway(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
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
			if identify.Op != protocol.AttachmentOpIdentify || identify.Agent == nil || identify.Agent.ID != "researcher" {
				t.Fatalf("unexpected identify %#v", identify)
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
				Cursor:    "evt_1",
				Event: &protocol.Event{
					ID:        "evt_1",
					Type:      protocol.EventTypeMessageCreated,
					NetworkID: "local",
					Message:   &protocol.Message{ID: "msg_1"},
				},
			}); err != nil {
				t.Fatalf("write event: %v", err)
			}

			var ack protocol.AttachmentFrame
			if err := connection.ReadJSON(&ack); err != nil {
				t.Fatalf("read ack: %v", err)
			}
			if ack.Op != protocol.AttachmentOpAck {
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
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   server.URL,
			NetworkID: "local",
		},
	}
	client := loop.NewMoltnetClient(config)

	var seen []protocol.Event
	if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error {
		seen = append(seen, event)
		return nil
	}); err != nil {
		t.Fatalf("StreamEvents() error = %v", err)
	}
	if len(seen) != 1 || seen[0].ID != "evt_1" {
		t.Fatalf("unexpected events %#v", seen)
	}
}
