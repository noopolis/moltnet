package loop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMoltnetClientKeepsAttachmentAliveWhileHandlerBlocked(t *testing.T) {
	t.Parallel()

	unblock := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/attach" {
			response.WriteHeader(http.StatusNotFound)
			return
		}

		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer connection.Close()

		if err := connection.WriteJSON(protocol.AttachmentFrame{
			Op:                  protocol.AttachmentOpHello,
			Version:             protocol.AttachmentProtocolV1,
			HeartbeatIntervalMS: 5000,
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

		for _, cursor := range []string{"evt_1", "evt_2"} {
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpEvent,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: "local",
				Cursor:    cursor,
				Event: &protocol.Event{
					ID:        cursor,
					Type:      protocol.EventTypeMessageCreated,
					NetworkID: "local",
					Message:   &protocol.Message{ID: "msg_" + cursor},
				},
			}); err != nil {
				t.Fatalf("write event %s: %v", cursor, err)
			}
		}

		if err := connection.WriteJSON(protocol.AttachmentFrame{
			Op:      protocol.AttachmentOpPing,
			Version: protocol.AttachmentProtocolV1,
		}); err != nil {
			t.Fatalf("write ping: %v", err)
		}

		if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}

		var pong protocol.AttachmentFrame
		if err := connection.ReadJSON(&pong); err != nil {
			t.Fatalf("read pong: %v", err)
		}
		if pong.Op != protocol.AttachmentOpPong {
			t.Fatalf("expected pong while handler blocked, got %#v", pong)
		}

		close(unblock)

		var ack protocol.AttachmentFrame
		if err := connection.ReadJSON(&ack); err != nil {
			t.Fatalf("read ack 1: %v", err)
		}
		if ack.Op != protocol.AttachmentOpAck || ack.Cursor != "evt_1" {
			t.Fatalf("unexpected first ack %#v", ack)
		}

		if err := connection.ReadJSON(&ack); err != nil {
			t.Fatalf("read ack 2: %v", err)
		}
		if ack.Op != protocol.AttachmentOpAck || ack.Cursor != "evt_2" {
			t.Fatalf("unexpected second ack %#v", ack)
		}

		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
			time.Now().Add(time.Second),
		)
	}))
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   server.URL,
			NetworkID: "local",
		},
	}
	client := NewMoltnetClient(config)
	var seen []string

	if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error {
		seen = append(seen, event.ID)
		if event.ID == "evt_1" {
			<-unblock
		}
		return nil
	}); err != nil {
		t.Fatalf("StreamEvents() error = %v", err)
	}

	if len(seen) != 2 || seen[0] != "evt_1" || seen[1] != "evt_2" {
		t.Fatalf("unexpected event order %#v", seen)
	}
}
