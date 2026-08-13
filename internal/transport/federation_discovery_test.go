package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestHTTPPairScopedRoomDiscoveryHonorsFederation(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		NetworkID: "local",
		Pairings:  []protocol.Pairing{{ID: "pair_remote", RemoteNetworkID: "remote"}},
		Store:     memory, Messages: memory, Broker: events.NewBroker(),
	})
	for _, request := range []protocol.CreateRoomRequest{
		{ID: "none", Federation: protocol.DefaultRoomFederation()},
		{ID: "all", Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationAll}},
		{ID: "listed", Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_remote"}}},
		{ID: "other", Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_other"}}},
	} {
		if _, err := service.CreateRoom(request); err != nil {
			t.Fatalf("CreateRoom(%q): %v", request.ID, err)
		}
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{
		ID: "pair_remote", Value: "pair-secret", Network: "remote", Scopes: []authn.Scope{authn.ScopePair},
	}}})
	if err != nil {
		t.Fatalf("NewPolicy(): %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/rooms", nil)
	request.Header.Set("Authorization", "Bearer pair-secret")
	response := httptest.NewRecorder()
	NewHTTPHandler(service, policy).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /v1/rooms status = %d, body = %s", response.Code, response.Body.String())
	}
	var page protocol.RoomPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode room page: %v", err)
	}
	if got := roomIDs(page.Rooms); !slices.Equal(got, []string{"all", "listed"}) {
		t.Fatalf("pair room discovery = %#v, want [all listed]", got)
	}
}

func roomIDs(rooms []protocol.Room) []string {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		ids = append(ids, room.ID)
	}
	return ids
}
