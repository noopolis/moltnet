package loop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunControlLoopPublishesPiControlResponse(t *testing.T) {
	t.Parallel()

	var published protocol.SendMessageRequest
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
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
			Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: "@researcher ping"}},
			CreatedAt: time.Now().UTC(),
		},
		CreatedAt: time.Now().UTC(),
	}

	moltnetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			connection, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				t.Fatalf("upgrade websocket: %v", err)
			}
			defer connection.Close()
			writeAttachmentHandshake(t, connection, "researcher")
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpEvent,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: "local",
				Cursor:    "evt_1",
				Event:     &event,
			}); err != nil {
				t.Fatalf("write event: %v", err)
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
			if err := json.NewDecoder(request.Body).Decode(&published); err != nil {
				t.Fatalf("decode published message: %v", err)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"message_id":"msg_2","event_id":"evt_2","accepted":true}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer moltnetServer.Close()

	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"from":"researcher","message":"@writer received"}`))
	}))
	defer controlServer.Close()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: moltnetServer.URL, NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimePi, ControlURL: controlServer.URL},
		Rooms:   []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeMentions}},
	}

	if err := RunControlLoop(context.Background(), config); err != nil {
		t.Fatalf("RunControlLoop() error = %v", err)
	}

	if published.From.ID != "researcher" || published.From.Name != "Researcher" {
		t.Fatalf("unexpected published actor %#v", published.From)
	}
	if published.Target.Kind != protocol.TargetKindRoom || published.Target.RoomID != "research" {
		t.Fatalf("unexpected published target %#v", published.Target)
	}
	if len(published.Parts) != 1 || published.Parts[0].Text != "@writer received" {
		t.Fatalf("unexpected published parts %#v", published.Parts)
	}
}

func writeAttachmentHandshake(t *testing.T, connection *websocket.Conn, agentID string) {
	t.Helper()
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
	if identify.Op != protocol.AttachmentOpIdentify || identify.Agent == nil || identify.Agent.ID != agentID {
		t.Fatalf("unexpected identify %#v", identify)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpReady,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		AgentID:   agentID,
	}); err != nil {
		t.Fatalf("write ready: %v", err)
	}
}
