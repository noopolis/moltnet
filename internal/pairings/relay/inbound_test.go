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

	response := relayInboundRoundTrip(t, handler, "correct-pair-token", "correct-pair-token", messageFrame(t))
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

	response := relayInboundRoundTrip(t, handler, "correct-pair-token", "wrong-token", messageFrame(t))
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

	// Before B20, the receiver's configured pairing token authenticated this
	// forged frame even though the frame itself carried no origin credential.
	response := relayInboundRoundTrip(t, handler, "correct-pair-token", "", frame)
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

func TestInboundRelayedRequestForUnknownPathIsForbidden(t *testing.T) {
	handler, service := newInboundHandlerFixture(t)
	defer service.Close()

	request := receivedRequest{
		header: frameHeader{Type: "req", ID: "unknown-path", Method: http.MethodGet, Path: "/v1/does-not-exist?source=relay"},
	}
	response := relayInboundRoundTrip(t, handler, "correct-pair-token", "correct-pair-token", request)
	if response.header.Type != "res" || response.header.ID != "unknown-path" {
		t.Fatalf("response header = %#v, want res for unknown-path", response.header)
	}
	if response.header.Status != http.StatusForbidden {
		t.Fatalf("response status = %d, want %d", response.header.Status, http.StatusForbidden)
	}
	if got, want := string(response.body), "relay inbound request forbidden\n"; got != want {
		t.Fatalf("response body = %q, want %q", got, want)
	}
}

func TestInboundHandlerRejectsNonAllowlistedPathWithoutInvokingHandler(t *testing.T) {
	invocations := 0
	handler := NewInboundHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invocations++ }))
	response, body := handler.serve(context.Background(), frameHeader{Method: http.MethodGet, Path: "/install.md"}, nil)
	if response.Status != http.StatusForbidden || string(body) != "relay inbound request forbidden\n" {
		t.Fatalf("response = (%d, %q), want forbidden response", response.Status, body)
	}
	if invocations != 0 {
		t.Fatalf("handler invocations = %d, want 0", invocations)
	}
}

func TestInboundHandlerForwardsAllowlistedRequests(t *testing.T) {
	var invocations int
	var gotMethod, gotPath string
	handler := NewInboundHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		invocations++
		gotMethod, gotPath = request.Method, request.URL.Path
		writer.WriteHeader(http.StatusNoContent)
	}))
	for _, request := range []frameHeader{
		{Method: http.MethodGet, Path: "/v1/network"}, {Method: http.MethodGet, Path: "/v1/rooms"},
		{Method: http.MethodGet, Path: "/v1/agents"}, {Method: http.MethodPost, Path: "/v1/messages"},
	} {
		response, _ := handler.serve(context.Background(), request, nil)
		if response.Status != http.StatusNoContent || gotMethod != request.Method || gotPath != request.Path {
			t.Fatalf("request %s %s = (%d, %s %s), want forwarded no-content request", request.Method, request.Path, response.Status, gotMethod, gotPath)
		}
	}
	if invocations != 4 {
		t.Fatalf("handler invocations = %d, want 4", invocations)
	}
}

func TestInboundHandlerRejectsWrongMethodWithoutInvokingHandler(t *testing.T) {
	invocations := 0
	handler := NewInboundHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invocations++ }))
	for _, method := range []string{http.MethodDelete, http.MethodPost} {
		response, _ := handler.serve(context.Background(), frameHeader{Method: method, Path: "/v1/rooms"}, nil)
		if response.Status != http.StatusForbidden {
			t.Fatalf("%s /v1/rooms status = %d, want %d", method, response.Status, http.StatusForbidden)
		}
	}
	if invocations != 0 {
		t.Fatalf("handler invocations = %d, want 0", invocations)
	}
}

func TestInboundHandlerRejectsPathEvasion(t *testing.T) {
	invocations := 0
	handler := NewInboundHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invocations++ }))
	response, _ := handler.serve(context.Background(), frameHeader{Method: http.MethodGet, Path: "/v1/rooms?x=1"}, nil)
	if response.Status != http.StatusOK || invocations != 1 {
		t.Fatalf("query request = (%d, %d invocations), want forwarded", response.Status, invocations)
	}
	for _, requestPath := range []string{"/v1/rooms/", "//v1/rooms", "/v1/rooms/../../console", "/v1/rooms/../../../etc/passwd", "/v1/rooms/%2e%2e/%2e%2e/console"} {
		response, _ = handler.serve(context.Background(), frameHeader{Method: http.MethodGet, Path: requestPath}, nil)
		if response.Status != http.StatusForbidden {
			t.Fatalf("GET %s status = %d, want %d", requestPath, response.Status, http.StatusForbidden)
		}
	}
	if invocations != 1 {
		t.Fatalf("handler invocations = %d, want 1", invocations)
	}
}

func TestInboundRelayedHandlerPanicReturnsInternalServerError(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	request := receivedRequest{
		header: frameHeader{Type: "req", ID: "panic-handler", Method: http.MethodGet, Path: "/v1/network"},
	}

	response := relayInboundRoundTrip(t, handler, "correct-pair-token", "correct-pair-token", request)
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

func TestInboundHandlerRejectsOversizedResponseBody(t *testing.T) {
	handler := NewInboundHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(bytes.Repeat([]byte("x"), maxInboundResponseBodyBytes+1))
	}))

	response, body := handler.serve(context.Background(), frameHeader{
		Type: "req", ID: "oversized-response", Method: http.MethodGet, Path: "/v1/network",
	}, nil)
	if response.Status != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want %d", response.Status, http.StatusInternalServerError)
	}
	if got, want := string(body), "relay response too large\n"; got != want {
		t.Fatalf("response body = %q, want %q", got, want)
	}
}

func TestInboundHandlerRejectsCumulativeOversizedResponseBody(t *testing.T) {
	chunk := bytes.Repeat([]byte("x"), 1024)
	handler := NewInboundHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		for written := 0; written <= maxInboundResponseBodyBytes; written += len(chunk) {
			_, _ = writer.Write(chunk)
		}
	}))

	response, body := handler.serve(context.Background(), frameHeader{
		Type: "req", ID: "cumulative-oversized-response", Method: http.MethodGet, Path: "/v1/network",
	}, nil)
	if response.Status != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want %d", response.Status, http.StatusInternalServerError)
	}
	if got, want := string(body), "relay response too large\n"; got != want {
		t.Fatalf("response body = %q, want %q", got, want)
	}
}

func TestInboundHandlerOversizedResponseOverridesHandlerStatus(t *testing.T) {
	handler := NewInboundHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write(bytes.Repeat([]byte("x"), maxInboundResponseBodyBytes+1))
	}))

	response, body := handler.serve(context.Background(), frameHeader{
		Type: "req", ID: "oversized-created-response", Method: http.MethodGet, Path: "/v1/network",
	}, nil)
	if response.Status != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want %d", response.Status, http.StatusInternalServerError)
	}
	if got, want := string(body), "relay response too large\n"; got != want {
		t.Fatalf("response body = %q, want %q", got, want)
	}
}

func TestInboundHandlerServesNormalResponseBody(t *testing.T) {
	expected := []byte(`{"parts":[{"kind":"text","text":"relay response"}],"artifacts":[{"id":"artifact-1"}]}`)
	handler := NewInboundHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write(expected)
	}))

	response, body := handler.serve(context.Background(), frameHeader{
		Type: "req", ID: "normal-response", Method: http.MethodPost, Path: "/v1/messages",
	}, nil)
	if response.Status != http.StatusCreated {
		t.Fatalf("response status = %d, want %d", response.Status, http.StatusCreated)
	}
	if !bytes.Equal(body, expected) {
		t.Fatalf("response body = %q, want %q", body, expected)
	}
}

func TestClientSetHandlerServesInboundRequests(t *testing.T) {
	client := NewClient("", "relay-connect-token", "correct-pair-token", "local")
	client.SetHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer correct-pair-token" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	response, _ := client.inboundResponse(context.Background(), frameHeader{
		Type: "req", ID: "late-handler", Auth: "correct-pair-token", Method: http.MethodPost, Path: "/v1/messages",
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
		// 7B.1: the "pair-token" credential below is now checked against
		// relay-room's federation stance (internal/rooms/federation_access.go),
		// so a matching pairing is configured here and the room is opened to
		// every pairing ("all") -- this fixture is about relay transport and
		// token auth, not federation.
		Pairings: []protocol.Pairing{{ID: "pair-token", RemoteNetworkID: "remote"}},
	})
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID: "relay-room", Members: []string{"remote:director"},
		Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationAll},
	}); err != nil {
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

func relayInboundRoundTrip(t *testing.T, handler http.Handler, receiverPairingToken, originatorCredential string, request receivedRequest) receivedRequest {
	t.Helper()
	request.header.Auth = originatorCredential
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

	client := NewClient(server.URL, "relay-connect-token", receiverPairingToken, "local", WithInboundHandler(handler))
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
