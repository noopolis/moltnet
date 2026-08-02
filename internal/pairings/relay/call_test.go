package relay

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync"
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

func TestClientCallFailsWhenConnectionDrops(t *testing.T) {
	hello := make(chan frameHeader, 1)
	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		readHello(t, conn, hello)
		_ = readRequest(t, conn)
		_ = conn.Close()
	})
	defer server.Close()

	client, stop := startRelayClient(t, server.URL)
	defer stop()
	select {
	case <-hello:
	case <-time.After(time.Second):
		t.Fatal("did not receive hello frame")
	}

	type callOutcome struct {
		err error
	}
	result := make(chan callOutcome, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		_, _, err := client.Call(ctx, http.MethodPost, "/v1/messages", []byte("request"))
		result <- callOutcome{err: err}
	}()

	select {
	case got := <-result:
		if !errors.Is(got.err, ErrConnectionLost) {
			t.Fatalf("Call() error = %v, want ErrConnectionLost", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call() did not return after the connection dropped")
	}
}

func TestClientSendsHelloBeforeConcurrentCall(t *testing.T) {
	firstFrame := make(chan frameHeader, 1)
	serverStop := make(chan struct{})
	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		header, _, err := parseFrame(raw)
		if err != nil {
			return
		}
		select {
		case firstFrame <- header:
		default:
		}
		<-serverStop
	})
	defer server.Close()
	defer close(serverStop)

	client := NewClient(server.URL, "relay-token", "juan-berlin")
	runDone := make(chan error, 1)
	stop := make(chan struct{})
	var hammers sync.WaitGroup
	for range 512 {
		hammers.Add(1)
		go func() {
			defer hammers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				_, _, _ = client.Call(ctx, http.MethodGet, "/v1/rooms", nil)
				cancel()
			}
		}()
	}
	go func() {
		runDone <- client.Run(context.Background())
	}()

	var first frameHeader
	gotFirstFrame := false
	select {
	case first = <-firstFrame:
		gotFirstFrame = true
	case <-time.After(2 * time.Second):
	}

	close(stop)
	hammers.Wait()
	client.Close()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Run() did not stop after Close()")
	}

	if !gotFirstFrame {
		t.Fatal("did not receive a relay frame")
	}
	if first.Type != "hello" {
		t.Fatalf("first relay frame type = %q, want hello", first.Type)
	}
}
