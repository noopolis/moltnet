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
				Op:      protocol.AttachmentOpError,
				Version: protocol.AttachmentProtocolV1,
				Error:   "runtime blocked",
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
			var errorFrame protocol.AttachmentFrame
			if err := connection.ReadJSON(&errorFrame); err != nil {
				t.Fatalf("read handler error frame: %v", err)
			}
			if errorFrame.Op != protocol.AttachmentOpError ||
				errorFrame.Version != protocol.AttachmentProtocolV1 ||
				!strings.Contains(errorFrame.Error, "context canceled") {
				t.Fatalf("unexpected handler error frame %#v", errorFrame)
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

func TestMoltnetClientRejectsHelloVersionBeforeIdentify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		hello protocol.AttachmentFrame
	}{
		{
			name:  "missing",
			hello: protocol.AttachmentFrame{Op: protocol.AttachmentOpHello},
		},
		{
			name: "mismatch",
			hello: protocol.AttachmentFrame{
				Op:      protocol.AttachmentOpHello,
				Version: "moltnet.attach.v2",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			identified := make(chan protocol.AttachmentFrame, 1)
			handlerDone := make(chan struct{})
			server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
				defer close(handlerDone)
				if err := connection.WriteJSON(test.hello); err != nil {
					t.Errorf("write hello: %v", err)
					return
				}
				if err := connection.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
					t.Errorf("set read deadline: %v", err)
					return
				}
				var identify protocol.AttachmentFrame
				if err := connection.ReadJSON(&identify); err == nil {
					identified <- identify
				}
			})
			defer server.Close()

			config := attachmentBranchTestConfig(server.URL)
			client := NewMoltnetClient(config)
			err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil })
			if err == nil || !strings.Contains(err.Error(), "HELLO version") {
				t.Fatalf("expected HELLO version error, got %v", err)
			}

			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for attachment handler")
			}
			select {
			case identify := <-identified:
				t.Fatalf("client sent IDENTIFY after incompatible HELLO: %#v", identify)
			default:
			}
		})
	}
}

func TestMoltnetClientRejectsReadyCompatibilityBeforeEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ready protocol.AttachmentFrame
		want  string
	}{
		{
			name: "missing version",
			ready: protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpReady,
				NetworkID: "local",
				AgentID:   "researcher",
			},
			want: "READY version",
		},
		{
			name: "mismatched version",
			ready: protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpReady,
				Version:   "moltnet.attach.v2",
				NetworkID: "local",
				AgentID:   "researcher",
			},
			want: "READY version",
		},
		{
			name: "mismatched network",
			ready: protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpReady,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: "remote",
				AgentID:   "researcher",
			},
			want: "READY network_id",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
				writeHello(t, connection)
				readIdentify(t, connection)
				if err := connection.WriteJSON(test.ready); err != nil {
					t.Fatalf("write ready: %v", err)
				}
				_ = connection.WriteJSON(attachmentBranchTestEventFrame(protocol.AttachmentProtocolV1))
			})
			defer server.Close()

			config := attachmentBranchTestConfig(server.URL)
			client := NewMoltnetClient(config)
			handled := make(chan protocol.Event, 1)
			err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error {
				handled <- event
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
			select {
			case event := <-handled:
				t.Fatalf("processed event before READY compatibility failure: %#v", event)
			default:
			}
		})
	}
}

func TestMoltnetClientRejectsUnversionedPostReadyFrames(t *testing.T) {
	t.Parallel()

	tests := []protocol.AttachmentFrame{
		{Op: protocol.AttachmentOpEvent},
		{Op: protocol.AttachmentOpPing},
		{Op: protocol.AttachmentOpError, Error: "runtime blocked"},
	}

	for _, frame := range tests {
		frame := frame
		t.Run(frame.Op, func(t *testing.T) {
			t.Parallel()

			server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
				writeHello(t, connection)
				readIdentify(t, connection)
				writeReady(t, connection)
				if err := connection.WriteJSON(frame); err != nil {
					t.Fatalf("write frame: %v", err)
				}
			})
			defer server.Close()

			config := attachmentBranchTestConfig(server.URL)
			client := NewMoltnetClient(config)
			err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error { return nil })
			if err == nil || !strings.Contains(err.Error(), "version is required") {
				t.Fatalf("expected version error, got %v", err)
			}
		})
	}
}

func TestMoltnetClientRejectsPostReadyFrameVersionMismatch(t *testing.T) {
	t.Parallel()

	server := newAttachmentTestServer(t, func(connection *websocket.Conn) {
		writeHello(t, connection)
		readIdentify(t, connection)
		writeReady(t, connection)
		if err := connection.WriteJSON(attachmentBranchTestEventFrame("moltnet.attach.v2")); err != nil {
			t.Fatalf("write event: %v", err)
		}
	})
	defer server.Close()

	config := attachmentBranchTestConfig(server.URL)
	client := NewMoltnetClient(config)
	handled := make(chan protocol.Event, 1)
	err := client.StreamEvents(context.Background(), config, func(event protocol.Event) error {
		handled <- event
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "EVENT version") {
		t.Fatalf("expected post-ready version error, got %v", err)
	}
	select {
	case event := <-handled:
		t.Fatalf("processed mismatched-version event: %#v", event)
	default:
	}
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
		Rooms: []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeAll}},
	}) {
		t.Fatal("expected auto-reply room binding to advertise thread support")
	}
	if attachmentSupportsThreads(bridgeconfig.Config{
		Rooms: []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeNever}},
	}) {
		t.Fatal("expected manual-only room binding to suppress thread support")
	}
}

func attachmentBranchTestConfig(baseURL string) bridgeconfig.Config {
	return bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: baseURL, NetworkID: "local"},
	}
}

func attachmentBranchTestEventFrame(version string) protocol.AttachmentFrame {
	return protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpEvent,
		Version:   version,
		NetworkID: "local",
		Cursor:    "evt_1",
		Event: &protocol.Event{
			ID:        "evt_1",
			Type:      protocol.EventTypeMessageCreated,
			NetworkID: "local",
			Message:   &protocol.Message{ID: "msg_1"},
		},
	}
}
