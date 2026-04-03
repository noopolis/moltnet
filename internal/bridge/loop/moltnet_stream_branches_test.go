package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMoltnetClientPostReadyErrorAndHandlerError(t *testing.T) {
	t.Parallel()

	t.Run("post-ready error", func(t *testing.T) {
		t.Parallel()

		server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
			writeHello(t, connection)
			readIdentify(t, connection)
			writeReady(t, connection)
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:    protocol.AttachmentOpError,
				Error: "runtime blocked",
			}); err != nil {
				t.Fatalf("write error frame: %v", err)
			}
		})
		defer server.Close()

		config := bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		}
		client := NewMoltnetClient(config)
		if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "runtime blocked") {
			t.Fatalf("expected post-ready error, got %v", err)
		}
	})

	t.Run("handler error", func(t *testing.T) {
		t.Parallel()

		server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
			writeHello(t, connection)
			readIdentify(t, connection)
			writeReady(t, connection)
			if err := connection.WriteJSON(protocol.AttachmentFrame{
				Op:      protocol.AttachmentOpEvent,
				Version: protocol.AttachmentProtocolV1,
				Cursor:  "evt_1",
				Event: &protocol.Event{
					ID:        "evt_1",
					Type:      protocol.EventTypeMessageCreated,
					NetworkID: "local",
					Message:   &protocol.Message{ID: "msg_1"},
				},
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
		expected := context.Canceled
		if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return expected }); err != expected {
			t.Fatalf("expected handler error %v, got %v", expected, err)
		}
	})
}

func TestMoltnetClientReadyMismatchAndContextCancel(t *testing.T) {
	t.Parallel()

	t.Run("ready mismatch", func(t *testing.T) {
		t.Parallel()

		server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
			writeHello(t, connection)
			readIdentify(t, connection)
			if err := connection.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpPing}); err != nil {
				t.Fatalf("write ping: %v", err)
			}
		})
		defer server.Close()

		config := bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		}
		client := NewMoltnetClient(config)
		if err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "expected READY frame") {
			t.Fatalf("expected ready mismatch error, got %v", err)
		}
	})

	t.Run("context cancel", func(t *testing.T) {
		t.Parallel()

		wait := make(chan struct{})
		connected := make(chan struct{})
		server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
			writeHello(t, connection)
			readIdentify(t, connection)
			writeReady(t, connection)
			close(connected)
			<-wait
		})
		defer server.Close()

		config := bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		}
		client := NewMoltnetClient(config)
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() {
			done <- client.StreamEvents(ctx, config, func(event protocol.Event) error { return nil })
		}()

		<-connected
		cancel()
		close(wait)

		if err := <-done; err != nil {
			t.Fatalf("expected nil on context cancel, got %v", err)
		}
	})
}

func TestAttachmentURLPathJoin(t *testing.T) {
	t.Parallel()

	endpoint, err := attachmentURL("https://example.com/base/")
	if err != nil {
		t.Fatalf("attachmentURL() error = %v", err)
	}
	if endpoint != "wss://example.com/base/v1/attach" {
		t.Fatalf("unexpected endpoint %q", endpoint)
	}
}

func TestAttachmentHeartbeatHelpers(t *testing.T) {
	t.Parallel()

	if got := heartbeatInterval(0); got != defaultAttachmentReadTimeout/2 {
		t.Fatalf("unexpected default heartbeat interval %v", got)
	}
	if got := heartbeatReadTimeout(3 * time.Second); got != 10*time.Second {
		t.Fatalf("unexpected minimum read timeout %v", got)
	}
	if got := heartbeatReadTimeout(20 * time.Second); got != 40*time.Second {
		t.Fatalf("unexpected scaled read timeout %v", got)
	}
	if !attachmentSupportsThreads(bridgeconfig.Config{
		Rooms: []bridgeconfig.RoomBinding{{ID: "research", Reply: bridgeconfig.ReplyAuto}},
	}) {
		t.Fatal("expected auto-reply room binding to advertise thread support")
	}
	if attachmentSupportsThreads(bridgeconfig.Config{
		Rooms: []bridgeconfig.RoomBinding{{ID: "research", Reply: bridgeconfig.ReplyManual}},
	}) {
		t.Fatal("expected manual-only room binding to suppress thread support")
	}
}
