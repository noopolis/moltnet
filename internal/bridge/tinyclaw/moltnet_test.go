package tinyclaw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMoltnetClient(t *testing.T) {
	t.Parallel()

	var authHeader string
	var messageAuth string

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/events/stream":
			authHeader = request.Header.Get("Authorization")
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = response.Write([]byte("event: message.created\n"))
			_, _ = response.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"message.created\",\"network_id\":\"local\",\"message\":{\"id\":\"msg_1\"}}\n\n"))
		case "/v1/messages":
			messageAuth = request.Header.Get("Authorization")
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"message_id":"msg_2","event_id":"evt_2","accepted":true}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newMoltnetClient(bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   server.URL,
			NetworkID: "local",
			Token:     "secret",
		},
	})

	var seen []moltnetEvent
	if err := client.streamEvents(context.Background(), func(event moltnetEvent) error {
		seen = append(seen, event)
		return nil
	}); err != nil {
		t.Fatalf("streamEvents() error = %v", err)
	}
	if authHeader != "Bearer secret" || len(seen) != 1 || seen[0].ID != "evt_1" {
		t.Fatalf("unexpected stream state auth=%q seen=%#v", authHeader, seen)
	}

	accepted, err := client.sendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("sendMessage() error = %v", err)
	}
	if messageAuth != "Bearer secret" || !accepted.Accepted {
		t.Fatalf("unexpected send result auth=%q accepted=%#v", messageAuth, accepted)
	}

	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.applyAuth(request)
	if request.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("unexpected auth header %q", request.Header.Get("Authorization"))
	}
}

func TestMoltnetClientErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/events/stream":
			response.WriteHeader(http.StatusBadGateway)
		case "/v1/messages":
			response.WriteHeader(http.StatusBadGateway)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newMoltnetClient(bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   server.URL,
			NetworkID: "local",
		},
	})

	if err := client.streamEvents(context.Background(), func(event moltnetEvent) error { return nil }); err == nil {
		t.Fatal("expected stream error")
	}

	if _, err := client.sendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	}); err == nil {
		t.Fatal("expected send error")
	}

	if _, err := decodeEventPayload([]string{"not-json"}); err == nil {
		t.Fatal("expected decode error")
	}

	noAuth := newMoltnetClient(bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
	})
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	noAuth.applyAuth(request)
	if request.Header.Get("Authorization") != "" {
		t.Fatalf("unexpected auth header %q", request.Header.Get("Authorization"))
	}

	badBase := &moltnetClient{httpClient: &http.Client{}, baseURL: ":"}
	if err := badBase.streamEvents(context.Background(), func(event moltnetEvent) error { return nil }); err == nil {
		t.Fatal("expected bad base url stream error")
	}
}

func TestMoltnetClientStreamHandlerErrorAndSendDecodeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/events/stream":
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = response.Write([]byte("event: message.created\n"))
			_, _ = response.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"message.created\",\"network_id\":\"local\"}\n\n"))
		case "/v1/messages":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`not-json`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newMoltnetClient(bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
	})

	expected := context.Canceled
	if err := client.streamEvents(context.Background(), func(event moltnetEvent) error { return expected }); err == nil {
		t.Fatal("expected handler error")
	}

	if _, err := client.sendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	}); err == nil {
		t.Fatal("expected decode error")
	}

	if _, err := client.sendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "researcher"},
		Parts: []protocol.Part{{
			Kind: "data",
			Data: map[string]any{"bad": make(chan int)},
		}},
	}); err == nil {
		t.Fatal("expected encode error")
	}
}
