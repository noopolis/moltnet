package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAttachmentEndpoint(t *testing.T) {
	t.Parallel()

	stream := make(chan protocol.Event, 1)
	service := &fakeService{
		network: protocol.Network{
			ID: "local",
		},
		stream: stream,
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if hello.Op != protocol.AttachmentOpHello || hello.Version != protocol.AttachmentProtocolV1 {
		t.Fatalf("unexpected hello %#v", hello)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent: &protocol.Actor{
			Type: "agent",
			ID:   "researcher",
		},
	}); err != nil {
		t.Fatalf("write identify: %v", err)
	}

	var ready protocol.AttachmentFrame
	if err := connection.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}
	if ready.Op != protocol.AttachmentOpReady || ready.AgentID != "researcher" {
		t.Fatalf("unexpected ready %#v", ready)
	}

	stream <- protocol.Event{
		ID:        "evt_1",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message:   &protocol.Message{ID: "msg_1"},
	}
	close(stream)

	var eventFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&eventFrame); err != nil {
		t.Fatalf("read event frame: %v", err)
	}
	if eventFrame.Op != protocol.AttachmentOpEvent || eventFrame.Event == nil || eventFrame.Event.ID != "evt_1" {
		t.Fatalf("unexpected event frame %#v", eventFrame)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:      protocol.AttachmentOpAck,
		Version: protocol.AttachmentProtocolV1,
		Cursor:  "evt_1",
	}); err != nil {
		t.Fatalf("write ack: %v", err)
	}
}

func TestAttachmentEndpointRejectsWrongNetwork(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "remote",
		Agent:     &protocol.Actor{ID: "researcher"},
	}); err != nil {
		t.Fatalf("write identify: %v", err)
	}

	var errorFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError {
		t.Fatalf("unexpected error frame %#v", errorFrame)
	}
}

func TestAttachmentEndpointRejectsUnexpectedIdentifySequence(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:      protocol.AttachmentOpReady,
		Version: protocol.AttachmentProtocolV1,
	}); err != nil {
		t.Fatalf("write invalid identify replacement: %v", err)
	}

	var errorFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError {
		t.Fatalf("unexpected error frame %#v", errorFrame)
	}
}

func TestAttachmentEndpointRejectsInvalidAgentID(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent:     &protocol.Actor{ID: "bad id"},
	}); err != nil {
		t.Fatalf("write identify: %v", err)
	}

	var errorFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError || !bytes.Contains([]byte(errorFrame.Error), []byte("agent.id")) {
		t.Fatalf("unexpected error frame %#v", errorFrame)
	}
}

func TestAttachmentEndpointRejectsMalformedJSONFrames(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	if err := connection.WriteMessage(websocket.TextMessage, []byte(`{"op":"IDENTIFY"`)); err != nil {
		t.Fatalf("write malformed identify: %v", err)
	}

	var errorFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError || errorFrame.Error != "attachment frame must be valid JSON" {
		t.Fatalf("unexpected error frame %#v", errorFrame)
	}
}

func TestAttachmentEndpointRejectsBinaryFrames(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	if err := connection.WriteMessage(websocket.BinaryMessage, []byte(`{}`)); err != nil {
		t.Fatalf("write binary frame: %v", err)
	}

	var errorFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError || errorFrame.Error != "attachment protocol only accepts text JSON frames" {
		t.Fatalf("unexpected error frame %#v", errorFrame)
	}
}

func TestAttachmentEndpointRespondsToPing(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent:     &protocol.Actor{ID: "researcher"},
	}); err != nil {
		t.Fatalf("write identify: %v", err)
	}
	var ready protocol.AttachmentFrame
	if err := connection.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}

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
}

func TestAttachmentEndpointRejectsUnknownAckCursor(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent:     &protocol.Actor{ID: "researcher"},
	}); err != nil {
		t.Fatalf("write identify: %v", err)
	}
	var ready protocol.AttachmentFrame
	if err := connection.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:      protocol.AttachmentOpAck,
		Version: protocol.AttachmentProtocolV1,
		Cursor:  "evt_missing",
	}); err != nil {
		t.Fatalf("write ack: %v", err)
	}

	var errorFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError {
		t.Fatalf("unexpected error frame %#v", errorFrame)
	}
}

func TestAttachmentEndpointSendsHeartbeatPing(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent:     &protocol.Actor{ID: "researcher"},
	}); err != nil {
		t.Fatalf("write identify: %v", err)
	}
	var ready protocol.AttachmentFrame
	if err := connection.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}

	if err := connection.SetReadDeadline(time.Now().Add(4 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var ping protocol.AttachmentFrame
	if err := connection.ReadJSON(&ping); err != nil {
		t.Fatalf("read heartbeat ping: %v", err)
	}
	if ping.Op != protocol.AttachmentOpPing {
		t.Fatalf("unexpected heartbeat frame %#v", ping)
	}
}

func TestIdentifiedAgentValidation(t *testing.T) {
	t.Parallel()

	if _, err := identifiedAgent(protocol.AttachmentFrame{}, "local"); err == nil {
		t.Fatal("expected missing identify error")
	}

	if _, err := identifiedAgent(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "remote",
		Agent:     &protocol.Actor{ID: "researcher"},
	}, "local"); err == nil {
		t.Fatal("expected network mismatch error")
	}

	if _, err := identifiedAgent(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent:     &protocol.Actor{},
	}, "local"); err == nil {
		t.Fatal("expected missing agent id error")
	}

	agent, err := identifiedAgent(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent:     &protocol.Actor{ID: "researcher"},
	}, "local")
	if err != nil {
		t.Fatalf("identifiedAgent() error = %v", err)
	}
	if agent.Type != "agent" {
		t.Fatalf("expected default agent type, got %#v", agent)
	}
}

func TestReadAttachmentFrameInvalidJSON(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer connection.Close()

		if err := connection.WriteMessage(websocket.TextMessage, []byte(`not-json`)); err != nil {
			t.Fatalf("write invalid payload: %v", err)
		}
	}))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	if _, err := readAttachmentFrame(connection, time.Second); err == nil {
		t.Fatal("expected invalid json error")
	}
}

func TestAttachmentEndpointRejectsOversizedFrame(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}

	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	payload := bytes.Repeat([]byte("a"), maxAttachmentFrameBytes+1024)
	if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write oversized frame: %v", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	_, _, err = connection.ReadMessage()
	if err == nil {
		t.Fatal("expected oversized frame to close connection")
	}
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.CloseMessageTooBig {
		t.Fatalf("expected close message too big error, got %v", err)
	}
}

func TestAttachmentTimingHelpers(t *testing.T) {
	t.Parallel()

	if got := attachmentHeartbeatInterval(); got != 5*time.Second {
		t.Fatalf("unexpected heartbeat interval %v", got)
	}
	if got := attachmentReadTimeout(); got != 10*time.Second {
		t.Fatalf("unexpected read timeout %v", got)
	}
}

func TestAttachmentEndpointClosesOnContextDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := make(chan protocol.Event)
	service := &fakeService{
		network: protocol.Network{ID: "local"},
		stream:  stream,
	}
	handler := NewHTTPHandler(service, nil)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		handler.ServeHTTP(response, request.WithContext(ctx))
	}))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent:     &protocol.Actor{ID: "researcher"},
	}); err != nil {
		t.Fatalf("write identify: %v", err)
	}
	var ready protocol.AttachmentFrame
	if err := connection.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}

	cancel()
}

func TestAttachmentWriterWritesJSON(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	received := make(chan protocol.AttachmentFrame, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer connection.Close()

		var frame protocol.AttachmentFrame
		if err := connection.ReadJSON(&frame); err != nil {
			t.Fatalf("read frame: %v", err)
		}
		received <- frame
	}))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	writer := &attachmentWriter{connection: connection}
	if err := writer.write(protocol.AttachmentFrame{
		Op:      protocol.AttachmentOpAck,
		Version: protocol.AttachmentProtocolV1,
		Cursor:  "evt_1",
	}); err != nil {
		t.Fatalf("writer.write() error = %v", err)
	}

	select {
	case frame := <-received:
		payload, err := json.Marshal(frame)
		if err != nil {
			t.Fatalf("marshal received frame: %v", err)
		}
		if string(payload) == "" || frame.Op != protocol.AttachmentOpAck {
			t.Fatalf("unexpected received frame %#v", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for written frame")
	}
}
