package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClientRoundTrip(t *testing.T) {
	hello := make(chan frameHeader, 1)
	requests := make(chan receivedRequest, 1)
	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		readHello(t, conn, hello)
		request := readRequest(t, conn)
		requests <- request

		frame, err := makeFrame(frameHeader{Type: "res", ID: request.header.ID, Status: http.StatusCreated}, []byte("relay response"))
		if err != nil {
			t.Errorf("make response frame: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Errorf("write response frame: %v", err)
		}
	})
	defer server.Close()

	client, stop := startRelayClient(t, server.URL)
	defer stop()

	select {
	case got := <-hello:
		if got.Network != "juan-berlin" {
			t.Fatalf("hello network = %q, want juan-berlin", got.Network)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive hello frame")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, body, err := client.Call(ctx, http.MethodGet, "/v1/rooms", []byte("request body"))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("Call() status = %d, want %d", status, http.StatusCreated)
	}
	if string(body) != "relay response" {
		t.Fatalf("Call() body = %q, want relay response", body)
	}

	select {
	case got := <-requests:
		if got.header.Method != http.MethodGet || got.header.Path != "/v1/rooms" {
			t.Fatalf("request header = %#v", got.header)
		}
		if string(got.body) != "request body" {
			t.Fatalf("request body = %q, want request body", got.body)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive request")
	}
}

func TestClientDropsUnknownResponseID(t *testing.T) {
	hello := make(chan frameHeader, 1)
	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		readHello(t, conn, hello)
		request := readRequest(t, conn)

		unknown, err := makeFrame(frameHeader{Type: "res", ID: "unknown-request", Status: http.StatusOK}, []byte("ignore"))
		if err != nil {
			t.Errorf("make unknown response: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, unknown); err != nil {
			t.Errorf("write unknown response: %v", err)
			return
		}

		response, err := makeFrame(frameHeader{Type: "res", ID: request.header.ID, Status: http.StatusOK}, []byte("expected"))
		if err != nil {
			t.Errorf("make response: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, response); err != nil {
			t.Errorf("write response: %v", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, body, err := client.Call(ctx, http.MethodPost, "/v1/messages", []byte("request"))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if status != http.StatusOK || string(body) != "expected" {
		t.Fatalf("Call() = (%d, %q), want (200, expected)", status, body)
	}
}

type receivedRequest struct {
	header frameHeader
	body   []byte
}

func newRelayTestServer(t *testing.T, serve func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer relay-token" {
			http.Error(writer, fmt.Sprintf("Authorization = %q", got), http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade relay connection: %v", err)
			return
		}
		defer conn.Close()
		serve(conn)
	}))
	server.URL = "ws" + strings.TrimPrefix(server.URL, "http")
	return server
}

func startRelayClient(t *testing.T, relayURL string) (*Client, func()) {
	t.Helper()
	client := NewClient(relayURL, "relay-token", "juan-berlin")
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

func readHello(t *testing.T, conn *websocket.Conn, hello chan<- frameHeader) {
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
		return
	}
	hello <- header
}

func readRequest(t *testing.T, conn *websocket.Conn) receivedRequest {
	t.Helper()
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Errorf("read request: %v", err)
		return receivedRequest{}
	}
	header, body, err := parseFrame(raw)
	if err != nil {
		t.Errorf("parse request: %v", err)
		return receivedRequest{}
	}
	if header.Type != "req" {
		t.Errorf("request type = %q, want req", header.Type)
	}
	return receivedRequest{header: header, body: body}
}
