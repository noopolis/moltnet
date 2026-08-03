package pairings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/internal/pairings/relay"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRelayTransportRoundTripsPairingRequests(t *testing.T) {
	requests := make(chan relayTransportRequest, 4)
	server, connected := newRelayTransportServer(t, func(request relayTransportRequest) (int, []byte) {
		requests <- request
		switch request.Method {
		case http.MethodGet:
			switch request.Path {
			case "/v1/network":
				return http.StatusOK, []byte(`{"id":"remote","name":"Remote"}`)
			case "/v1/rooms":
				return http.StatusOK, []byte(`{"rooms":[{"id":"research","network_id":"remote"}]}`)
			case "/v1/agents":
				return http.StatusOK, []byte(`{"agents":[{"id":"writer","network_id":"remote"}]}`)
			}
		case http.MethodPost:
			if request.Path == "/v1/messages" {
				return http.StatusAccepted, []byte(`{"message_id":"msg_local_1","event_id":"evt_remote_1","accepted":true}`)
			}
		}
		return http.StatusNotFound, []byte(`{"error":"not found"}`)
	})
	defer server.Close()

	relayClient, stopRelay := startRelayTransportClient(t, server.URL)
	defer stopRelay()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("relay client did not connect")
	}
	waitForRelayTransportConnection(t, relayClient)

	client := NewClient()
	pairing := protocol.Pairing{ID: "relay-remote", Relay: &protocol.PairingRelay{URL: server.URL}}
	client.RegisterRelay(pairing.ID, relayClient)

	network, err := client.FetchNetwork(context.Background(), pairing)
	if err != nil || network.ID != "remote" {
		t.Fatalf("FetchNetwork() = %#v, %v", network, err)
	}
	rooms, err := client.FetchRooms(context.Background(), pairing)
	if err != nil || len(rooms) != 1 || rooms[0].ID != "research" {
		t.Fatalf("FetchRooms() = %#v, %v", rooms, err)
	}
	agents, err := client.FetchAgents(context.Background(), pairing)
	if err != nil || len(agents) != 1 || agents[0].ID != "writer" {
		t.Fatalf("FetchAgents() = %#v, %v", agents, err)
	}
	accepted, err := client.RelayMessage(context.Background(), pairing, protocol.SendMessageRequest{
		ID:     "msg_local_1",
		Origin: protocol.MessageOrigin{NetworkID: "local", MessageID: "msg_local_1"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "writer", NetworkID: "local"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	})
	if err != nil || !accepted.Accepted || accepted.MessageID != "msg_local_1" {
		t.Fatalf("RelayMessage() = %#v, %v", accepted, err)
	}

	want := []struct{ method, path string }{
		{http.MethodGet, "/v1/network"},
		{http.MethodGet, "/v1/rooms"},
		{http.MethodGet, "/v1/agents"},
		{http.MethodPost, "/v1/messages"},
	}
	for _, expected := range want {
		select {
		case request := <-requests:
			if request.Method != expected.method || request.Path != expected.path {
				t.Fatalf("relay request = (%s, %s), want (%s, %s)", request.Method, request.Path, expected.method, expected.path)
			}
			if request.Method == http.MethodPost {
				var payload protocol.SendMessageRequest
				if err := json.Unmarshal(request.Body, &payload); err != nil {
					t.Fatalf("decode relayed message request: %v", err)
				}
				if payload.ID != "msg_local_1" {
					t.Fatalf("relayed message ID = %q, want msg_local_1", payload.ID)
				}
			}
		case <-time.After(time.Second):
			t.Fatalf("did not receive relay request for %s %s", expected.method, expected.path)
		}
	}
}

func TestRelayTransportReturnsErrorsForUnavailableClients(t *testing.T) {
	pairing := protocol.Pairing{ID: "relay-remote", Relay: &protocol.PairingRelay{URL: "ws://relay.example.com"}}

	if _, err := NewClient().FetchNetwork(context.Background(), pairing); err == nil {
		t.Fatal("FetchNetwork() succeeded without a registered relay client")
	}

	client := NewClient()
	client.RegisterRelay(pairing.ID, relay.NewClient("", "", "", "local"))
	if _, err := client.FetchNetwork(context.Background(), pairing); !errors.Is(err, relay.ErrNotConnected) {
		t.Fatalf("FetchNetwork() error = %v, want ErrNotConnected", err)
	}
}

type relayTransportRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   []byte
}

func newRelayTransportServer(t *testing.T, respond func(relayTransportRequest) (int, []byte)) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	connected := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer relay-token" {
			http.Error(writer, "unexpected authorization", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade relay connection: %v", err)
			return
		}
		defer conn.Close()

		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("read relay hello: %v", err)
			return
		}
		select {
		case connected <- struct{}{}:
		default:
		}
		for {
			_, frame, err := conn.ReadMessage()
			if err != nil {
				return
			}
			request, err := parseRelayTransportRequest(frame)
			if err != nil {
				t.Errorf("parse relay request: %v", err)
				return
			}
			status, body := http.StatusOK, []byte(`{}`)
			if request.Path != "/_relay_transport_ready" {
				status, body = respond(request)
			}
			response, err := makeRelayTransportResponse(request.ID, status, body)
			if err != nil {
				t.Errorf("make relay response: %v", err)
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, response); err != nil {
				return
			}
		}
	}))
	server.URL = "ws" + strings.TrimPrefix(server.URL, "http")
	return server, connected
}

func startRelayTransportClient(t *testing.T, relayURL string) (*relay.Client, func()) {
	t.Helper()
	client := relay.NewClient(relayURL, "relay-token", "pairing-token", "local")
	runDone := make(chan error, 1)
	go func() { runDone <- client.Run(context.Background()) }()

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

func waitForRelayTransportConnection(t *testing.T, client *relay.Client) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		status, _, err := client.Call(ctx, http.MethodGet, "/_relay_transport_ready", nil)
		cancel()
		if errors.Is(err, relay.ErrNotConnected) {
			time.Sleep(time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatalf("wait for relay connection: %v", err)
		}
		if status != http.StatusOK {
			t.Fatalf("relay readiness status = %d, want %d", status, http.StatusOK)
		}
		return
	}
	t.Fatal("relay client did not become ready")
}

func parseRelayTransportRequest(frame []byte) (relayTransportRequest, error) {
	header, body, found := bytes.Cut(frame, []byte{'\n'})
	if !found {
		header = frame
	}
	var request relayTransportRequest
	if err := json.Unmarshal(header, &request); err != nil {
		return relayTransportRequest{}, err
	}
	request.Body = body
	return request, nil
}

func makeRelayTransportResponse(id string, status int, body []byte) ([]byte, error) {
	header, err := json.Marshal(struct {
		Type   string `json:"t"`
		ID     string `json:"id"`
		Status int    `json:"status"`
	}{Type: "res", ID: id, Status: status})
	if err != nil {
		return nil, err
	}
	return append(append(header, '\n'), body...), nil
}
