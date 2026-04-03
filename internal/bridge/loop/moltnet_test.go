package loop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMoltnetClientStreamAndSend(t *testing.T) {
	t.Parallel()

	var messageAuth string
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			if request.Header.Get("Authorization") != "Bearer secret" {
				t.Fatalf("expected auth header, got %q", request.Header.Get("Authorization"))
			}

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
			if ack.Op != protocol.AttachmentOpAck || ack.Cursor != "evt_1" {
				t.Fatalf("unexpected ack %#v", ack)
			}

			_ = connection.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
				timeNow(),
			)
		case "/v1/messages":
			messageAuth = request.Header.Get("Authorization")
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"message_id":"msg_2","event_id":"evt_2","accepted":true}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   server.URL,
			NetworkID: "local",
			Token:     "secret",
		},
		DMs: &bridgeconfig.DMConfig{Enabled: true},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research"},
		},
	}
	client := NewMoltnetClient(config)

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

	accepted, err := client.SendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if messageAuth != "Bearer secret" || !accepted.Accepted {
		t.Fatalf("unexpected send result auth=%q accepted=%#v", messageAuth, accepted)
	}
}

func TestMoltnetClientErrors(t *testing.T) {
	t.Parallel()

	badAttach := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusBadGateway)
	}))
	defer badAttach.Close()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: badAttach.URL, NetworkID: "local", Token: "secret"},
	}
	client := NewMoltnetClient(config)
	if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "request moltnet attach") {
		t.Fatalf("expected attach request error, got %v", err)
	}

	invalidAttach := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer connection.Close()
		if err := connection.WriteMessage(websocket.TextMessage, []byte(`not-json`)); err != nil {
			t.Fatalf("write invalid frame: %v", err)
		}
	}))
	defer invalidAttach.Close()

	config.Moltnet.BaseURL = invalidAttach.URL
	config.Moltnet.Token = ""
	client = NewMoltnetClient(config)
	if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "decode attachment frame") {
		t.Fatalf("expected attach decode error, got %v", err)
	}

	binaryAttach := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer connection.Close()
		if err := connection.WriteMessage(websocket.BinaryMessage, []byte(`{}`)); err != nil {
			t.Fatalf("write binary frame: %v", err)
		}
	}))
	defer binaryAttach.Close()

	config.Moltnet.BaseURL = binaryAttach.URL
	client = NewMoltnetClient(config)
	if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "text JSON frames") {
		t.Fatalf("expected binary frame error, got %v", err)
	}

	failingSend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("expected auth header on send, got %q", request.Header.Get("Authorization"))
		}
		response.WriteHeader(http.StatusBadGateway)
	}))
	defer failingSend.Close()

	client = NewMoltnetClient(bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: failingSend.URL, NetworkID: "local", Token: "secret"},
	})
	_, err := client.SendMessage(context.Background(), protocol.SendMessageRequest{
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
	})
	if err == nil || !strings.Contains(err.Error(), "moltnet message send returned") {
		t.Fatalf("expected send error, got %v", err)
	}

	invalidSend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"message_id":`))
	}))
	defer invalidSend.Close()

	client = NewMoltnetClient(bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: invalidSend.URL, NetworkID: "local"},
	})
	_, err = client.SendMessage(context.Background(), protocol.SendMessageRequest{
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
	})
	if err == nil || !strings.Contains(err.Error(), "decode moltnet message response") {
		t.Fatalf("expected send decode error, got %v", err)
	}

	if _, err := attachmentURL(":"); err == nil {
		t.Fatal("expected invalid attachment url error")
	}
	if _, err := attachmentURL("ftp://example.com"); err == nil {
		t.Fatal("expected unsupported scheme error")
	}

	_, err = client.SendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts: []protocol.Part{{
			Kind: "data",
			Data: map[string]any{"bad": make(chan int)},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "encode moltnet message") {
		t.Fatalf("expected encode error, got %v", err)
	}
}

func TestMoltnetClientStreamBranches(t *testing.T) {
	t.Parallel()

	t.Run("hello mismatch", func(t *testing.T) {
		t.Parallel()

		server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
			if err := connection.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpReady}); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		})
		defer server.Close()

		config := bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		}
		client := NewMoltnetClient(config)
		if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "expected HELLO frame") {
			t.Fatalf("expected hello mismatch error, got %v", err)
		}
	})

	t.Run("ready error", func(t *testing.T) {
		t.Parallel()

		server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
			writeHello(t, connection)
			readIdentify(t, connection)
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:    protocol.AttachmentOpError,
				Error: "bad attachment",
			}); err != nil {
				t.Fatalf("write error: %v", err)
			}
		})
		defer server.Close()

		config := bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		}
		client := NewMoltnetClient(config)
		if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "bad attachment") {
			t.Fatalf("expected ready error, got %v", err)
		}
	})

	t.Run("event without payload", func(t *testing.T) {
		t.Parallel()

		server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
			writeHello(t, connection)
			readIdentify(t, connection)
			writeReady(t, connection)
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:      protocol.AttachmentOpEvent,
				Version: protocol.AttachmentProtocolV1,
				Cursor:  "evt_1",
			}); err != nil {
				t.Fatalf("write event: %v", err)
			}
		})
		defer server.Close()

		config := bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		}
		client := NewMoltnetClient(config)
		if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "missing event payload") {
			t.Fatalf("expected missing event payload error, got %v", err)
		}
	})

	t.Run("ping pong", func(t *testing.T) {
		t.Parallel()

		server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
			writeHello(t, connection)
			readIdentify(t, connection)
			writeReady(t, connection)
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:      protocol.AttachmentOpPing,
				Version: protocol.AttachmentProtocolV1,
			}); err != nil {
				t.Fatalf("write ping: %v", err)
			}

			var pong protocol.AttachmentFrame
			if err := connection.ReadJSON(&pong); err != nil {
				t.Fatalf("read pong: %v", err)
			}
			if pong.Op != protocol.AttachmentOpPong {
				t.Fatalf("unexpected pong %#v", pong)
			}

			_ = connection.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
				timeNow(),
			)
		})
		defer server.Close()

		config := bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		}
		client := NewMoltnetClient(config)
		if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err != nil {
			t.Fatalf("expected clean ping/pong stream, got %v", err)
		}
	})
}

func timeNow() time.Time {
	return time.Now().Add(time.Second)
}

func newAttachmentTestServer(t *testing.T, handle func(connection *websocket.Conn)) *httptest.Server {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer connection.Close()
		handle(connection)
	}))
}

func writeHello(t *testing.T, connection *websocket.Conn) {
	t.Helper()

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:                  protocol.AttachmentOpHello,
		Version:             protocol.AttachmentProtocolV1,
		HeartbeatIntervalMS: 30000,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
}

func readIdentify(t *testing.T, connection *websocket.Conn) protocol.AttachmentFrame {
	t.Helper()

	var identify protocol.AttachmentFrame
	if err := connection.ReadJSON(&identify); err != nil {
		t.Fatalf("read identify: %v", err)
	}
	return identify
}

func writeReady(t *testing.T, connection *websocket.Conn) {
	t.Helper()

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpReady,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		AgentID:   "researcher",
	}); err != nil {
		t.Fatalf("write ready: %v", err)
	}
}
