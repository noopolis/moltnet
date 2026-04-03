package pairings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestClientFetchesRemoteData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected auth header %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1/network":
			_, _ = response.Write([]byte(`{"id":"remote","name":"Remote"}`))
		case "/v1/rooms":
			_, _ = response.Write([]byte(`{"rooms":[{"id":"research","network_id":"remote"}]}`))
		case "/v1/agents":
			_, _ = response.Write([]byte(`{"agents":[{"id":"writer","network_id":"remote"}]}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient()
	pairing := protocol.Pairing{RemoteBaseURL: server.URL, Token: "secret"}

	network, err := client.FetchNetwork(context.Background(), pairing)
	if err != nil || network.ID != "remote" {
		t.Fatalf("FetchNetwork() = %#v, %v", network, err)
	}

	rooms, err := client.FetchRooms(context.Background(), pairing)
	if err != nil || len(rooms) != 1 || rooms[0].NetworkID != "remote" {
		t.Fatalf("FetchRooms() = %#v, %v", rooms, err)
	}

	agents, err := client.FetchAgents(context.Background(), pairing)
	if err != nil || len(agents) != 1 || agents[0].NetworkID != "remote" {
		t.Fatalf("FetchAgents() = %#v, %v", agents, err)
	}
}

func TestClientErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient()
	_, err := client.FetchNetwork(context.Background(), protocol.Pairing{RemoteBaseURL: server.URL})
	if err == nil {
		t.Fatal("expected pairing client error")
	}
}

func TestAuthorizeRequest(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	authorizeRequest(request, protocol.Pairing{Token: "secret"})
	if request.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("unexpected auth header %q", request.Header.Get("Authorization"))
	}

	request = httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	authorizeRequest(request, protocol.Pairing{})
	if request.Header.Get("Authorization") != "" {
		t.Fatalf("expected empty auth header, got %q", request.Header.Get("Authorization"))
	}
}

func TestClientHandlesDecodeErrorAndTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/network" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		_, _ = response.Write([]byte("{"))
	}))
	defer server.Close()

	client := NewClient()
	_, err := client.FetchNetwork(context.Background(), protocol.Pairing{RemoteBaseURL: server.URL + "/"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClientRoomAndAgentDecodeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/rooms", "/v1/agents":
			_, _ = response.Write([]byte(`{`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient()
	if _, err := client.FetchRooms(context.Background(), protocol.Pairing{RemoteBaseURL: server.URL}); err == nil {
		t.Fatal("expected rooms decode error")
	}
	if _, err := client.FetchAgents(context.Background(), protocol.Pairing{RemoteBaseURL: server.URL}); err == nil {
		t.Fatal("expected agents decode error")
	}
}

func TestClientRelaysMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected relay path %q", request.URL.Path)
		}
		if request.Method != http.MethodPost {
			t.Fatalf("unexpected relay method %q", request.Method)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected auth header %q", request.Header.Get("Authorization"))
		}
		_, _ = response.Write([]byte(`{"message_id":"msg_net_a_1","event_id":"evt_net_b_1","accepted":true}`))
	}))
	defer server.Close()

	client := NewClient()
	accepted, err := client.RelayMessage(context.Background(), protocol.Pairing{RemoteBaseURL: server.URL, Token: "secret"}, protocol.SendMessageRequest{
		ID:     "msg_net_a_1",
		Origin: protocol.MessageOrigin{NetworkID: "net_a", MessageID: "msg_net_a_1"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha", NetworkID: "net_a"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("RelayMessage() error = %v", err)
	}
	if !accepted.Accepted || accepted.MessageID != "msg_net_a_1" {
		t.Fatalf("unexpected relay acceptance %#v", accepted)
	}
}

func TestClientRelayErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/messages":
			response.WriteHeader(http.StatusBadGateway)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient()
	_, err := client.RelayMessage(context.Background(), protocol.Pairing{RemoteBaseURL: server.URL}, protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	})
	if err == nil {
		t.Fatal("expected relay error")
	}
}

func TestClientRejectsMalformedRemoteBaseURL(t *testing.T) {
	t.Parallel()

	client := NewClient()
	pairing := protocol.Pairing{RemoteBaseURL: "http://[::1"}

	if _, err := client.FetchNetwork(context.Background(), pairing); err == nil {
		t.Fatal("expected malformed network request error")
	}
	if _, err := client.FetchRooms(context.Background(), pairing); err == nil {
		t.Fatal("expected malformed rooms request error")
	}
	if _, err := client.FetchAgents(context.Background(), pairing); err == nil {
		t.Fatal("expected malformed agents request error")
	}
	if _, err := client.RelayMessage(context.Background(), pairing, protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	}); err == nil {
		t.Fatal("expected malformed relay request error")
	}
}
