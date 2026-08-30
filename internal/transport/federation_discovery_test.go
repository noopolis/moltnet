package transport

import (
	"context"
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

// TestHTTPPairScopedRoomDiscoveryResolvesUnboundInviterCredential is P1-1's
// required regression (final-gate review, confirmed live against a real
// server): an inviter-issued pairing credential has a TokenID equal to its
// pairing's id, but `pair invite` never persists a network binding on the
// inviter side (claims.Network() stays empty forever for that credential --
// see PairingForPairScopedContext's doc comment, federation_access.go).
// filterPairScopedRoomDiscovery used to resolve pairing identity by
// claims.Network() alone, so this exact, common credential shape saw an
// empty room list from this handler even though rooms.Service's own gate
// (ListRoomsContext, applied first) would have allowed its federated rooms.
// The fix delegates to service.PairingForPairScopedContext, which resolves
// by TokenID first -- this test's token carries no Network at all, so it
// only passes once that delegation is in place.
func TestHTTPPairScopedRoomDiscoveryResolvesUnboundInviterCredential(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		NetworkID: "local",
		Pairings:  []protocol.Pairing{{ID: "pair_remote", RemoteNetworkID: "remote"}},
		Store:     memory, Messages: memory, Broker: events.NewBroker(),
	})
	for _, request := range []protocol.CreateRoomRequest{
		{ID: "shared", Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_remote"}}},
		{ID: "unshared", Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_other"}}},
	} {
		if _, err := service.CreateRoom(request); err != nil {
			t.Fatalf("CreateRoom(%q): %v", request.ID, err)
		}
	}
	// Deliberately no Network on this token: exactly the inviter-side
	// credential shape `pair invite` mints, with TokenID equal to its
	// pairing's id and no confirmed network binding.
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{
		ID: "pair_remote", Value: "pair-secret", Scopes: []authn.Scope{authn.ScopePair},
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
	if got := roomIDs(page.Rooms); !slices.Equal(got, []string{"shared"}) {
		t.Fatalf("unbound inviter credential GET /v1/rooms = %#v, want [shared]", got)
	}
}

// TestHTTPOperatorNotLockedOutOfRoomDiscovery is F1's required regression at
// the actual HTTP boundary (review round 2): TestOperatorNotLockedOutByPairScopedFix
// (internal/rooms/federation_access_test.go) calls ListRoomsContext
// directly, so it sits below filterPairScopedRoomDiscovery -- the second
// federation gate this handler applies after the service's own filter -- and
// never exercised it. A [observe, write, admin, pair] operator token (the
// exact shape `--bearer` init mints) must see every room the service itself
// already returned, not have the whole page nulled out by this second gate.
func TestHTTPOperatorNotLockedOutOfRoomDiscovery(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		NetworkID: "local",
		Store:     memory, Messages: memory, Broker: events.NewBroker(),
	})
	for _, request := range []protocol.CreateRoomRequest{
		{ID: "general", Visibility: protocol.RoomVisibilityPublic, Federation: protocol.DefaultRoomFederation()},
		{ID: "shared", Visibility: protocol.RoomVisibilityPublic, Federation: protocol.DefaultRoomFederation()},
	} {
		if _, err := service.CreateRoom(request); err != nil {
			t.Fatalf("CreateRoom(%q): %v", request.ID, err)
		}
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{
		ID: "operator", Value: "operator-secret",
		Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin, authn.ScopePair},
	}}})
	if err != nil {
		t.Fatalf("NewPolicy(): %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/rooms", nil)
	request.Header.Set("Authorization", "Bearer operator-secret")
	response := httptest.NewRecorder()
	NewHTTPHandler(service, policy).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /v1/rooms status = %d, body = %s", response.Code, response.Body.String())
	}
	var page protocol.RoomPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode room page: %v", err)
	}
	if got := roomIDs(page.Rooms); !slices.Equal(got, []string{"general", "shared"}) {
		t.Fatalf("operator GET /v1/rooms = %#v, want [general shared] (not locked out by the pair-scoped filter)", got)
	}
}

func roomIDs(rooms []protocol.Room) []string {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		ids = append(ids, room.ID)
	}
	return ids
}

// TestFilterPairScopedRoomDiscoveryOperatorBoundary drives
// filterPairScopedRoomDiscovery directly, against a stand-in Service, so it
// isolates *this* gate from internal/rooms' own federation filter. The two
// HTTP-level tests above go through a real rooms.Service, which applies its
// own gate first -- so if only this transport-side operator check diverged,
// those tests would still pass on the rooms-side filtering alone and this
// second gate could rot unnoticed. That is precisely how the two gates
// diverged before (P1-1), and why this one is here at all: it exists to hold
// when the Service in front of it does not.
//
// "Operator" is authn.Claims.Operator -- admin AND write -- shared with
// internal/rooms rather than copied into this package as it once was. Both
// rows below fail if a divergent copy is re-inlined here: an operator must
// keep the whole page, and a `[write, pair]` peer must not be mistaken for
// one.
func TestFilterPairScopedRoomDiscoveryOperatorBoundary(t *testing.T) {
	t.Parallel()

	service := &fakeService{pairings: []protocol.Pairing{{ID: "pair_remote", RemoteNetworkID: "remote"}}}
	unfiltered := protocol.RoomPage{Rooms: []protocol.Room{
		{ID: "none", Federation: protocol.DefaultRoomFederation()},
		{ID: "listed", Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_remote"}}},
	}}

	for _, testCase := range []struct {
		name    string
		tokenID string
		scopes  []authn.Scope
		want    []string
	}{{
		name:    "legacy four-scope operator keeps every room",
		tokenID: "operator",
		scopes:  []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin, authn.ScopePair},
		want:    []string{"none", "listed"},
	}, {
		name:    "write-scoped peer credential is still a peer",
		tokenID: "pair_remote",
		scopes:  []authn.Scope{authn.ScopeWrite, authn.ScopePair},
		want:    []string{"listed"},
	}, {
		name:    "plain peer credential is filtered",
		tokenID: "pair_remote",
		scopes:  []authn.Scope{authn.ScopePair},
		want:    []string{"listed"},
	}} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
				ID: testCase.tokenID, Network: "remote", Scopes: testCase.scopes,
			}))
			page, err := filterPairScopedRoomDiscovery(ctx, service, unfiltered)
			if err != nil {
				t.Fatalf("filterPairScopedRoomDiscovery(): %v", err)
			}
			if got := roomIDs(page.Rooms); !slices.Equal(got, testCase.want) {
				t.Fatalf("filterPairScopedRoomDiscovery() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}
