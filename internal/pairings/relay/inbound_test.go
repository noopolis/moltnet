package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/internal/transport"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestInboundRelayedMessageWithValidPairTokenIsPersisted(t *testing.T) {
	handler, service := newInboundHandlerFixture(t)
	defer service.Close()

	response := relayInboundRoundTrip(t, handler, "correct-pair-token", messageFrame(t))
	if response.header.Status != http.StatusAccepted {
		t.Fatalf("response status = %d, want %d", response.header.Status, http.StatusAccepted)
	}
	if got := inboundMessageCount(t, service); got != 1 {
		t.Fatalf("stored message count = %d, want 1", got)
	}
}

func TestInboundRelayedMessageWithWrongTokenIsRejected(t *testing.T) {
	handler, service := newInboundHandlerFixture(t)
	defer service.Close()

	response := relayInboundRoundTrip(t, handler, "wrong-token", messageFrame(t))
	if response.header.Status != http.StatusUnauthorized && response.header.Status != http.StatusForbidden {
		t.Fatalf("response status = %d, want 401 or 403", response.header.Status)
	}
	if got := inboundMessageCount(t, service); got != 0 {
		t.Fatalf("stored message count = %d, want 0", got)
	}
}

func TestInboundRelayedMessageWithNoTokenIsRejected(t *testing.T) {
	handler, service := newInboundHandlerFixture(t)
	defer service.Close()
	frame := messageFrame(t)

	response := relayInboundRoundTrip(t, handler, " ", frame)
	if response.header.Status != http.StatusUnauthorized {
		t.Fatalf("response status = %d, want %d", response.header.Status, http.StatusUnauthorized)
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.body, &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Code != "unauthorized" || body.Error != "authorization required" {
		t.Fatalf("response body = %#v, want unauthorized authorization error", body)
	}
	if got := inboundMessageCount(t, service); got != 0 {
		t.Fatalf("stored message count = %d, want 0", got)
	}
}

func TestInboundRelayedRequestForUnknownPathReturnsHandlerStatus(t *testing.T) {
	handler, service := newInboundHandlerFixture(t)
	defer service.Close()

	request := receivedRequest{
		header: frameHeader{Type: "req", ID: "unknown-path", Method: http.MethodGet, Path: "/v1/does-not-exist?source=relay"},
	}
	expected := httptest.NewRecorder()
	handler.ServeHTTP(expected, httptest.NewRequest(request.header.Method, request.header.Path, nil))
	if expected.Code != http.StatusTemporaryRedirect {
		t.Fatalf("direct handler status = %d, want %d", expected.Code, http.StatusTemporaryRedirect)
	}

	response := relayInboundRoundTrip(t, handler, "correct-pair-token", request)
	if response.header.Type != "res" || response.header.ID != "unknown-path" {
		t.Fatalf("response header = %#v, want res for unknown-path", response.header)
	}
	if response.header.Status != expected.Code {
		t.Fatalf("response status = %d, want handler status %d", response.header.Status, expected.Code)
	}
	if !bytes.Equal(response.body, expected.Body.Bytes()) {
		t.Fatalf("response body = %q, want handler body %q", response.body, expected.Body.Bytes())
	}
}

func TestInboundRelayedHandlerPanicReturnsInternalServerError(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	request := receivedRequest{
		header: frameHeader{Type: "req", ID: "panic-handler", Method: http.MethodGet, Path: "/panic"},
	}

	response := relayInboundRoundTrip(t, handler, "correct-pair-token", request)
	if response.header.Type != "res" || response.header.ID != "panic-handler" {
		t.Fatalf("response header = %#v, want res for panic-handler", response.header)
	}
	if response.header.Status != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want %d", response.header.Status, http.StatusInternalServerError)
	}
	if got, want := string(response.body), "internal error\n"; got != want {
		t.Fatalf("response body = %q, want %q", got, want)
	}
}

func TestClientSetHandlerServesInboundRequests(t *testing.T) {
	client := NewClient("", "correct-pair-token", "local")
	client.SetHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer correct-pair-token" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	response, _ := client.inboundResponse(context.Background(), frameHeader{
		Type: "req", ID: "late-handler", Method: http.MethodPost, Path: "/v1/messages",
	}, nil)
	if response.Status != http.StatusNoContent {
		t.Fatalf("response status = %d, want %d", response.Status, http.StatusNoContent)
	}
}

func newInboundHandlerFixture(t *testing.T) (http.Handler, *rooms.Service) {
	t.Helper()
	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		Store:       memory,
		Messages:    memory,
		Broker:      events.NewBroker(),
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
	})
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "relay-room", Members: []string{"remote:director"}}); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{
		ID: "pair-token", Value: "correct-pair-token", Scopes: []authn.Scope{authn.ScopePair},
	}}})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	return transport.NewHTTPHandler(service, policy), service
}

func messageFrame(t *testing.T) receivedRequest {
	t.Helper()
	body, err := json.Marshal(protocol.SendMessageRequest{
		ID:     "relay-message",
		Origin: protocol.MessageOrigin{NetworkID: "remote", MessageID: "remote-message"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "relay-room"},
		From:   protocol.Actor{Type: "agent", ID: "director", NetworkID: "remote"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "relayed message"}},
	})
	if err != nil {
		t.Fatalf("marshal message request: %v", err)
	}
	return receivedRequest{header: frameHeader{Type: "req", ID: "inbound-message", Method: http.MethodPost, Path: "/v1/messages"}, body: body}
}

func inboundMessageCount(t *testing.T, service *rooms.Service) int {
	t.Helper()
	page, err := service.ListRoomMessagesContext(context.Background(), "relay-room", protocol.PageRequest{})
	if err != nil {
		t.Fatalf("ListRoomMessagesContext() error = %v", err)
	}
	return len(page.Messages)
}

func relayInboundRoundTrip(t *testing.T, handler http.Handler, token string, request receivedRequest) receivedRequest {
	t.Helper()
	responses := make(chan receivedRequest, 1)
	server := newInboundRelayTestServer(t, func(conn *websocket.Conn) {
		hello := make(chan frameHeader, 1)
		readHello(t, conn, hello)

		frame, err := makeFrame(request.header, request.body)
		if err != nil {
			t.Errorf("make inbound request: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Errorf("write inbound request: %v", err)
			return
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read inbound response: %v", err)
			return
		}
		header, body, err := parseFrame(raw)
		if err != nil {
			t.Errorf("parse inbound response: %v", err)
			return
		}
		responses <- receivedRequest{header: header, body: body}
	})
	defer server.Close()

	client := NewClient(server.URL, token, "local", WithInboundHandler(handler))
	runDone := make(chan error, 1)
	go func() { runDone <- client.Run(context.Background()) }()
	defer func() {
		client.Close()
		select {
		case err := <-runDone:
			if err != nil {
				t.Errorf("Run() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Run() did not stop after Close()")
		}
	}()

	select {
	case response := <-responses:
		return response
	case <-time.After(time.Second):
		t.Fatal("did not receive inbound response")
		return receivedRequest{}
	}
}

func newInboundRelayTestServer(t *testing.T, serve func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
