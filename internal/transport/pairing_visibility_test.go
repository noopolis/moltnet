package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/pairings"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestHTTPPairingDiscoveryFiltersRestrictedAgentClaims(t *testing.T) {
	remoteRooms := []protocol.Room{
		{ID: "hidden", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "hidden"), Visibility: protocol.RoomVisibilityPrivate, Members: []string{"local:atlas"}},
		{ID: "self", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "self"), Visibility: protocol.RoomVisibilityPrivate, Members: []string{"local:luna", "remote:bob"}},
		{ID: "collision", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "collision"), Visibility: protocol.RoomVisibilityPrivate, Members: []string{"luna"}},
		{ID: "public", NetworkID: "remote", FQID: protocol.RoomFQID("remote", "public"), Visibility: protocol.RoomVisibilityPublic, Members: []string{"public-bot"}},
	}
	remoteAgents := []protocol.AgentSummary{
		{ID: "local:atlas", NetworkID: "local", FQID: protocol.AgentFQID("local", "atlas")},
		{ID: "local:luna", NetworkID: "local", FQID: protocol.AgentFQID("local", "luna")},
		{ID: "bob", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "bob")},
		{ID: "luna", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "luna")},
		{ID: "public-bot", NetworkID: "remote", FQID: protocol.AgentFQID("remote", "public-bot")},
	}
	remote := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/network":
			_ = json.NewEncoder(response).Encode(protocol.Network{
				ID: "remote", Version: "test",
				Protocols:    protocol.NetworkProtocols{HTTP: []string{protocol.HTTPProtocolV1}, Pair: []string{protocol.PairProtocolV1}},
				Capabilities: protocol.NetworkCapabilities{DirectMessages: true, MessagePagination: "cursor"},
			})
		case "/v1/rooms":
			_ = json.NewEncoder(response).Encode(protocol.RoomPage{Rooms: remoteRooms})
		case "/v1/agents":
			_ = json.NewEncoder(response).Encode(protocol.AgentPage{Agents: remoteAgents})
		default:
			http.NotFound(response, request)
		}
	}))
	defer remote.Close()

	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		NetworkID: "local",
		Pairings: []protocol.Pairing{{
			ID:              "pair_1",
			RemoteNetworkID: "remote",
			RemoteBaseURL:   remote.URL,
			Status:          protocol.PairingStatusConnected,
		}},
		Store:         memory,
		Messages:      memory,
		Broker:        events.NewBroker(),
		PairingClient: pairings.NewClient(),
	})
	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{
			{ID: "luna", Value: "luna-secret", Scopes: []authn.Scope{authn.ScopeObserve}, Agents: []string{"luna"}},
			{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeAdmin}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy(): %v", err)
	}
	handler := NewHTTPHandler(service, policy)

	requestPage := func(path, token string, target any) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		if err := json.NewDecoder(response.Body).Decode(target); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}

	var restrictedRooms protocol.RoomPage
	requestPage("/v1/pairings/pair_1/rooms?limit=1", "luna-secret", &restrictedRooms)
	if len(restrictedRooms.Rooms) != 1 || restrictedRooms.Rooms[0].ID != "self" || !restrictedRooms.Page.HasMore {
		t.Fatalf("restricted rooms = %#v", restrictedRooms)
	}
	var restrictedAgents protocol.AgentPage
	requestPage("/v1/pairings/pair_1/agents?limit=1", "luna-secret", &restrictedAgents)
	if len(restrictedAgents.Agents) != 1 || restrictedAgents.Agents[0].ID != "local:luna" || !restrictedAgents.Page.HasMore {
		t.Fatalf("restricted agents = %#v", restrictedAgents)
	}

	var operatorRooms protocol.RoomPage
	requestPage("/v1/pairings/pair_1/rooms", "operator-secret", &operatorRooms)
	if len(operatorRooms.Rooms) != len(remoteRooms) {
		t.Fatalf("operator rooms = %#v", operatorRooms)
	}
	var operatorAgents protocol.AgentPage
	requestPage("/v1/pairings/pair_1/agents", "operator-secret", &operatorAgents)
	if len(operatorAgents.Agents) != len(remoteAgents) {
		t.Fatalf("operator agents = %#v", operatorAgents)
	}
}
