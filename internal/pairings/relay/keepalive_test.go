package relay

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	testPingInterval    = 40 * time.Millisecond
	testReadIdleTimeout = 200 * time.Millisecond
)

func TestClientReconnectsAfterSilentPeerReadIdleTimeout(t *testing.T) {
	firstHandlerRelease := make(chan struct{})
	firstHello := make(chan struct{})
	secondHello := make(chan struct{})
	var connections atomic.Int32
	var firstHelloOnce sync.Once
	var secondHelloOnce sync.Once

	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		connection := connections.Add(1)
		readKeepaliveHello(t, conn)
		if connection == 1 {
			firstHelloOnce.Do(func() { close(firstHello) })
			// Deliberately neither read nor close the first connection. This
			// suppresses gorilla's automatic pong and models a half-open peer.
			<-firstHandlerRelease
			return
		}

		secondHelloOnce.Do(func() { close(secondHello) })
		request := readRequest(t, conn)
		response, err := makeFrame(frameHeader{Type: "res", ID: request.header.ID, Status: http.StatusAccepted}, []byte("reconnected"))
		if err != nil {
			t.Errorf("make reconnect response: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, response); err != nil {
			t.Errorf("write reconnect response: %v", err)
		}
	})
	client, stop := startKeepaliveRelayClient(t, server.URL)
	defer func() {
		close(firstHandlerRelease)
		stop()
		server.Close()
	}()

	select {
	case <-firstHello:
	case <-time.After(time.Second):
		t.Fatal("first connection did not receive hello")
	}

	select {
	case <-secondHello:
	case <-time.After(testReadIdleTimeout * 5):
		t.Fatal("client did not reconnect after the silent peer exceeded its read-idle timeout")
	}
	waitForConnection(t, client, testReadIdleTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, body, err := client.Call(ctx, http.MethodGet, "/v1/reconnected", nil)
	if err != nil {
		t.Fatalf("Call() after reconnect error = %v", err)
	}
	if status != http.StatusAccepted || string(body) != "reconnected" {
		t.Fatalf("Call() after reconnect = (%d, %q), want (202, %q)", status, body, "reconnected")
	}
}

func TestClientKeepsHealthyIdleConnectionAlive(t *testing.T) {
	connected := make(chan struct{})
	var connections atomic.Int32
	var connectedOnce sync.Once

	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		connections.Add(1)
		readKeepaliveHello(t, conn)
		connectedOnce.Do(func() { close(connected) })
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			header, body, err := parseFrame(raw)
			if err != nil {
				t.Errorf("parse client frame: %v", err)
				return
			}
			if header.Type != "req" {
				t.Errorf("client frame type = %q, want req", header.Type)
				return
			}
			response, err := makeFrame(frameHeader{Type: "res", ID: header.ID, Status: http.StatusOK}, body)
			if err != nil {
				t.Errorf("make healthy response: %v", err)
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, response); err != nil {
				t.Errorf("write healthy response: %v", err)
				return
			}
		}
	})
	defer server.Close()

	client, stop := startKeepaliveRelayClient(t, server.URL)
	defer stop()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("connection did not receive hello")
	}

	// The server remains in ReadMessage, so gorilla's default ping handler runs
	// and sends pongs while there is no application traffic.
	<-time.After(testReadIdleTimeout*2 + testPingInterval)
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections after healthy idle period = %d, want 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, body, err := client.Call(ctx, http.MethodPost, "/v1/healthy", []byte("still connected"))
	if err != nil {
		t.Fatalf("Call() on healthy idle connection error = %v", err)
	}
	if status != http.StatusOK || string(body) != "still connected" {
		t.Fatalf("Call() on healthy idle connection = (%d, %q), want (200, %q)", status, body, "still connected")
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections after Call() = %d, want 1", got)
	}
}

func startKeepaliveRelayClient(t *testing.T, relayURL string) (*Client, func()) {
	t.Helper()
	client := NewClient(relayURL, "relay-token", "pairing-token", "juan-berlin", withKeepaliveDurations(testPingInterval, testReadIdleTimeout))
	runDone := make(chan error, 1)
	go func() {
		runDone <- client.Run(context.Background())
	}()

	return client, func() {
		client.Close()
		select {
		case err := <-runDone:
			if err != nil {
				t.Errorf("Run() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Run() did not stop after Close()")
		}
	}
}

func readKeepaliveHello(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Errorf("read hello: %v", err)
		return
	}
	header, payload, err := parseFrame(raw)
	if err != nil {
		t.Errorf("parse hello: %v", err)
		return
	}
	if header.Type != "hello" || len(payload) != 0 {
		t.Errorf("hello = (%#v, %q), want hello without payload", header, payload)
	}
}

func waitForConnection(t *testing.T, client *Client, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		if client.currentConnection() != nil {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("client did not install reconnected connection")
		case <-poll.C:
		}
	}
}
