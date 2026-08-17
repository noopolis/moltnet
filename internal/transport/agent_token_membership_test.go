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

// TestAgentTokenReadsPrivateRoomItIsAMemberOf is the positive case for
// TestSkillWithAgentTokenDoesNotExposePrivateMemberRooms
// (discovery_public_policy_test.go): internal/rooms/access_policy.go's
// canReadRoom grants read access to a private room by membership alone,
// independent of the token's scopes — even a write+attach-only agent token
// (pre-PLAN.md-phase-6a-review P2-2) already saw a private room it was a
// member of; adding `observe` (P2-2) does not change that, and must not
// additionally leak rooms the agent is *not* a member of (covered by the
// fallback fix in skill_markdown.go's canReadRoomFallback, exercised in the
// sibling test above). This drives the real rooms.Service, not fakeService,
// so it reflects actual production access, not the transport package's own
// approximation of it.
func TestAgentTokenReadsPrivateRoomItIsAMemberOf(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "public",
		NetworkName:       "Public",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})
	for _, room := range []protocol.CreateRoomRequest{
		{ID: "public-floor", Visibility: protocol.RoomVisibilityPublic, WritePolicy: protocol.RoomWritePolicyRegisteredAgents},
		{ID: "private-member-room", Visibility: protocol.RoomVisibilityPrivate, WritePolicy: protocol.RoomWritePolicyMembers, Members: []string{"guest"}},
		{ID: "private-other-room", Visibility: protocol.RoomVisibilityPrivate, WritePolicy: protocol.RoomWritePolicyMembers, Members: []string{"other-agent"}},
	} {
		if _, err := service.CreateRoom(room); err != nil {
			t.Fatalf("CreateRoom(%q) error = %v", room.ID, err)
		}
	}

	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	handler := NewHTTPHandler(service, policy)

	response := httptest.NewRecorder()
	registerRequest := httptest.NewRequest(http.MethodPost, "/v1/agents/register", strings.NewReader(`{"requested_agent_id":"guest"}`))
	registerRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, registerRequest)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected agent registration to succeed, got %d: %s", response.Code, response.Body.String())
	}
	var registration protocol.AgentRegistration
	if err := json.Unmarshal(response.Body.Bytes(), &registration); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	if registration.AgentToken == "" {
		t.Fatalf("expected a generated agent_token in the registration response: %s", response.Body.String())
	}

	response = httptest.NewRecorder()
	skillRequest := httptest.NewRequest(http.MethodGet, "/skill.md", nil)
	skillRequest.Header.Set("Authorization", "Bearer "+registration.AgentToken)
	handler.ServeHTTP(response, skillRequest)
	if response.Code != http.StatusOK {
		t.Fatalf("expected agent-token skill, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "private-member-room") {
		t.Fatalf("agent-token skill should show the private room this agent is a member of\n%s", body)
	}
	if strings.Contains(body, "private-other-room") {
		t.Fatalf("agent-token skill leaked a private room this agent is not a member of\n%s", body)
	}
}
