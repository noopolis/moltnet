package relay

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClientCallTimeoutDrainsPending(t *testing.T) {
	hello := make(chan frameHeader, 1)
	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		readHello(t, conn, hello)
		_ = readRequest(t, conn)
		<-time.After(time.Second)
	})
	defer server.Close()

	client, stop := startRelayClient(t, server.URL)
	defer stop()
	select {
	case <-hello:
	case <-time.After(time.Second):
		t.Fatal("did not receive hello frame")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := client.Call(ctx, http.MethodGet, "/v1/never", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want deadline exceeded", err)
	}

	client.pendingMu.Lock()
	defer client.pendingMu.Unlock()
	if len(client.pending) != 0 {
		t.Fatalf("len(client.pending) = %d, want 0", len(client.pending))
	}
}

func TestClientOpaquePayloadRoundTrip(t *testing.T) {
	hello := make(chan frameHeader, 1)
	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		readHello(t, conn, hello)
		request := readRequest(t, conn)
		frame, err := makeFrame(frameHeader{Type: "res", ID: request.header.ID, Status: http.StatusOK}, request.body)
		if err != nil {
			t.Errorf("make echo response: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Errorf("write echo response: %v", err)
		}
	})
	defer server.Close()

	client, stop := startRelayClient(t, server.URL)
	defer stop()
	select {
	case <-hello:
	case <-time.After(time.Second):
		t.Fatal("did not receive hello frame")
	}

	body := []byte{'n', 'o', 't', '-', 'j', 's', 'o', 'n', '-', '{', '{', '{', 0x00, 0xff}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, got, err := client.Call(ctx, http.MethodPut, "/v1/opaque", body)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("Call() status = %d, want 200", status)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("Call() body = %v, want %v", got, body)
	}
}
