package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestOpenPublicRouteAuthSemantics(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{network: protocol.Network{ID: "local"}}, policy)

	assertStatus(t, handler, http.MethodGet, "/v1/network", "", "", http.StatusOK)
	assertStatus(t, handler, http.MethodGet, "/v1/network", "Bearer wrong", "", http.StatusUnauthorized)
	assertStatus(t, handler, http.MethodGet, "/v1/dms", "", "", http.StatusUnauthorized)
	assertStatus(t, handler, http.MethodGet, "/v1/artifacts", "", "", http.StatusUnauthorized)
}

func TestOpenPublicEventStreamFiltersPrivateEvents(t *testing.T) {
	t.Parallel()

	stream := make(chan protocol.Event, 5)
	stream <- protocol.Event{
		ID:   "evt_room",
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			ID:     "msg_room",
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "agora"},
			Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "public room"}},
		},
	}
	stream <- protocol.Event{
		ID:   "evt_dm",
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			ID:     "msg_dm",
			Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1"},
			Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "private dm"}},
		},
	}
	stream <- protocol.Event{
		ID:      "evt_pair",
		Type:    protocol.EventTypePairingUpdated,
		Pairing: &protocol.Pairing{ID: "pair_1"},
	}
	stream <- protocol.Event{
		ID:   "evt_members",
		Type: protocol.EventTypeRoomMembersUpdated,
		Room: &protocol.Room{ID: "agora", Members: []string{"luna", "atlas"}},
	}
	stream <- protocol.Event{
		ID:   "evt_room_created",
		Type: protocol.EventTypeRoomCreated,
		Room: &protocol.Room{ID: "agora"},
	}
	close(stream)

	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local"},
		stream:  stream,
	}, policy)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected public stream status %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{"evt_room", "public room", "evt_room_created"} {
		if !strings.Contains(body, want) {
			t.Fatalf("public stream missing %q:\n%s", want, body)
		}
	}
	for _, blocked := range []string{"evt_dm", "private dm", "evt_pair", "pair_1", "evt_members", "room.members.updated"} {
		if strings.Contains(body, blocked) {
			t.Fatalf("public stream leaked %q:\n%s", blocked, body)
		}
	}
}

func TestOpenRegistrationAndSendHTTPFlow(t *testing.T) {
	t.Parallel()

	handler := newOpenHTTPHandlerForTest(t, nil)
	luna, code := registerAgentHTTP(t, handler, "", "luna")
	if code != http.StatusCreated || !strings.HasPrefix(luna.AgentToken, authn.AgentTokenPrefix) {
		t.Fatalf("unexpected open registration status=%d value=%#v", code, luna)
	}

	duplicate, code := registerAgentHTTP(t, handler, "", "luna")
	if code != http.StatusConflict || duplicate.AgentToken != "" {
		t.Fatalf("unexpected anonymous duplicate status=%d value=%#v", code, duplicate)
	}
	idempotent, code := registerAgentHTTP(t, handler, "Bearer "+luna.AgentToken, "luna")
	if code != http.StatusOK || idempotent.AgentToken != "" || idempotent.AgentID != "luna" {
		t.Fatalf("unexpected idempotent status=%d value=%#v", code, idempotent)
	}

	sendBody := `{"target":{"kind":"room","room_id":"agora"},"from":{"type":"agent","id":"luna"},"parts":[{"kind":"text","text":"hello"}]}`
	assertStatus(t, handler, http.MethodPost, "/v1/messages", "", sendBody, http.StatusUnauthorized)
	assertStatus(t, handler, http.MethodPost, "/v1/messages", "Bearer "+luna.AgentToken, sendBody, http.StatusAccepted)

	atlas, _ := registerAgentHTTP(t, handler, "", "atlas")
	assertStatus(t, handler, http.MethodPost, "/v1/messages", "Bearer "+atlas.AgentToken, sendBody, http.StatusConflict)
	assertStatus(t, handler, http.MethodGet, "/v1/dms", "Bearer "+luna.AgentToken, "", http.StatusForbidden)
}

func TestOpenStaticTokenOwnershipAndPairSpoofHTTP(t *testing.T) {
	t.Parallel()

	handler := newOpenHTTPHandlerForTest(t, []authn.TokenConfig{
		{ID: "writer", Value: "writer-secret", Scopes: []authn.Scope{authn.ScopeWrite}},
		{ID: "admin-writer", Value: "admin-write-secret", Scopes: []authn.Scope{authn.ScopeAdmin, authn.ScopeWrite}},
		{ID: "pair", Value: "pair-secret", Scopes: []authn.Scope{authn.ScopePair}},
		{ID: "attach-alpha", Value: "attach-secret", Scopes: []authn.Scope{authn.ScopeAttach}, Agents: []string{"alpha"}},
	})
	registerAgentHTTP(t, handler, "", "luna")
	sendBody := `{"target":{"kind":"room","room_id":"agora"},"from":{"type":"agent","id":"luna"},"parts":[{"kind":"text","text":"hello"}]}`
	assertStatus(t, handler, http.MethodPost, "/v1/messages", "Bearer writer-secret", sendBody, http.StatusConflict)
	assertStatus(t, handler, http.MethodPost, "/v1/messages", "Bearer admin-write-secret", sendBody, http.StatusAccepted)
	assertStatus(t, handler, http.MethodPost, "/v1/messages", "Bearer pair-secret", sendBody, http.StatusForbidden)

	registerBody := `{"requested_agent_id":"beta"}`
	assertStatus(t, handler, http.MethodPost, "/v1/agents/register", "Bearer attach-secret", registerBody, http.StatusForbidden)
}

func newOpenHTTPHandlerForTest(t *testing.T, tokens []authn.TokenConfig) http.Handler {
	t.Helper()

	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "agora"}); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen, ListenAddr: ":8787", Tokens: tokens})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	return NewHTTPHandler(service, policy)
}

func registerAgentHTTP(t *testing.T, handler http.Handler, authorization string, agentID string) (protocol.AgentRegistration, int) {
	t.Helper()

	body := `{"requested_agent_id":"` + agentID + `","name":"` + agentID + `"}`
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agents/register", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	handler.ServeHTTP(response, request)

	var registration protocol.AgentRegistration
	_ = json.NewDecoder(response.Body).Decode(&registration)
	return registration, response.Code
}

func assertStatus(t *testing.T, handler http.Handler, method string, path string, authorization string, body string, want int) {
	t.Helper()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s auth=%q status=%d want %d body=%s", method, path, authorization, response.Code, want, response.Body.String())
	}
}
