package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestDiscoveryRoutesBearerPublicReadWithOpenRegistration(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:              authn.ModeBearer,
		PublicRead:        true,
		AgentRegistration: authn.AgentRegistrationOpen,
		Tokens: []authn.TokenConfig{
			{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
			{ID: "admin", Value: "admin-secret", Scopes: []authn.Scope{authn.ScopeAdmin}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "public"},
		rooms: []protocol.Room{
			{
				ID:          "floor",
				Visibility:  protocol.RoomVisibilityPublic,
				WritePolicy: protocol.RoomWritePolicyMembers,
				Access:      &protocol.RoomAccess{CanRead: true, CanWrite: false, Reason: "public-read/members-write"},
			},
			{
				ID:          "guestbook",
				Visibility:  protocol.RoomVisibilityPublic,
				WritePolicy: protocol.RoomWritePolicyRegisteredAgents,
				Access:      &protocol.RoomAccess{CanRead: true, CanWrite: false, Reason: "public-read/registered_agents-write"},
			},
			{
				ID:          "ops",
				Visibility:  protocol.RoomVisibilityPrivate,
				WritePolicy: protocol.RoomWritePolicyMembers,
			},
		},
	}, policy)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://public.example/install.md", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected public install guide, got %d", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{
		"- Auth mode: `bearer`",
		"- Rooms accepting registered agents: `guestbook`",
		"--registration open",
		"auth_mode: open",
		"registration: open",
		"moltnet send --network public --target room:guestbook",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("install markdown missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "moltnet send --network public --target room:floor") {
		t.Fatalf("install markdown exposed member-only room send\n%s", body)
	}
	if strings.Contains(body, "ops") {
		t.Fatalf("anonymous install markdown exposed private room\n%s", body)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "https://public.example/install.md", nil)
	request.Header.Set("Authorization", "Bearer admin-secret")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected admin install guide, got %d", response.Code)
	}
	body = response.Body.String()
	if !strings.Contains(body, "Readable rooms: `floor`, `guestbook`, `ops`") {
		t.Fatalf("admin install markdown should include private room\n%s", body)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "https://public.example/llms.txt", nil)
	request.Header.Set("Authorization", "Bearer admin-secret")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected admin llms text, got %d", response.Code)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "https://public.example/skill.md", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected public skill, got %d", response.Code)
	}
	body = response.Body.String()
	for _, want := range []string{
		"Current access: public read access",
		"--registration open",
		"moltnet send --network public --target room:guestbook",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("skill markdown missing %q\n%s", want, body)
		}
	}
}

func TestGeneratedMarkdownUsesRoomWriteAccess(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	const agentToken = "magt_v1_guest"
	service := &fakeService{
		network: protocol.Network{ID: "public"},
		rooms: []protocol.Room{
			{
				ID:          "episode-floor",
				Visibility:  "public",
				WritePolicy: "members",
				Members:     []string{"socrates"},
				Access:      &protocol.RoomAccess{CanRead: true, CanWrite: false, Reason: "public-read/member-write"},
			},
			{
				ID:          "guestbook",
				Visibility:  "public",
				WritePolicy: "registered_agents",
				Members:     []string{"socrates"},
			},
		},
		agentTokenClaims: map[string]authn.Claims{
			agentToken: authn.NewAgentTokenClaims("guest", authn.AgentTokenCredentialKey(agentToken)),
		},
	}
	handler := NewHTTPHandler(service, policy)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://public.example/install.md", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected install status %d", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{
		"Readable rooms: `episode-floor`, `guestbook`",
		"Rooms accepting registered agents: `guestbook`",
		"Read-only for outside agents: `episode-floor`",
		"--registration open",
		"moltnet send --network public --target room:guestbook",
		"      - id: episode-floor\n        wake: never",
		"      - id: guestbook\n        wake: mentions",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("install markdown missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "moltnet send --network public --target room:episode-floor") {
		t.Fatalf("install markdown exposed member-only send\n%s", body)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/skill.md", nil)
	handler.ServeHTTP(response, request)
	body = response.Body.String()
	if strings.Contains(body, "moltnet send --network public --target room:episode-floor") {
		t.Fatalf("anonymous skill exposed member-only send\n%s", body)
	}
	if !strings.Contains(body, "moltnet send --network public --target room:guestbook") {
		t.Fatalf("anonymous skill should show registered-agent room after connect\n%s", body)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/skill.md", nil)
	request.Header.Set("Authorization", "Bearer "+agentToken)
	handler.ServeHTTP(response, request)
	body = response.Body.String()
	if !strings.Contains(body, "Writable rooms now: `guestbook`") ||
		!strings.Contains(body, "moltnet send --network public --target room:guestbook") ||
		strings.Contains(body, "moltnet send --network public --target room:episode-floor") {
		t.Fatalf("agent skill did not respect room access\n%s", body)
	}
}

func TestSkillWithAgentTokenDoesNotExposePrivateMemberRooms(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:              authn.ModeBearer,
		PublicRead:        true,
		AgentRegistration: authn.AgentRegistrationOpen,
		Tokens: []authn.TokenConfig{
			{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	const agentToken = "magt_v1_guest"
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{
			ID: "public",
			Capabilities: protocol.NetworkCapabilities{
				DirectMessages: true,
			},
		},
		rooms: []protocol.Room{
			{
				ID:          "public-floor",
				Visibility:  protocol.RoomVisibilityPublic,
				WritePolicy: protocol.RoomWritePolicyRegisteredAgents,
			},
			{
				ID:          "private-member-room",
				Visibility:  protocol.RoomVisibilityPrivate,
				WritePolicy: protocol.RoomWritePolicyMembers,
				Members:     []string{"guest"},
			},
		},
		agentTokenClaims: map[string]authn.Claims{
			agentToken: authn.NewAgentTokenClaims("guest", authn.AgentTokenCredentialKey(agentToken)),
		},
	}, policy)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/skill.md", nil)
	request.Header.Set("Authorization", "Bearer "+agentToken)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected agent-token skill, got %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Readable rooms: `public-floor`") ||
		!strings.Contains(body, "Writable rooms now: `public-floor`") {
		t.Fatalf("skill should show public writable room\n%s", body)
	}
	if strings.Contains(body, "private-member-room") ||
		strings.Contains(body, "Read a DM") {
		t.Fatalf("agent-token skill leaked private room or DM read instructions\n%s", body)
	}
	if !strings.Contains(body, "Send to a DM") {
		t.Fatalf("agent-token skill should keep write-scoped DM send instructions when DMs are enabled\n%s", body)
	}
}

func TestSkillWithAdminTokenShowsPrivateRooms(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{
			{ID: "admin", Value: "admin-secret", Scopes: []authn.Scope{authn.ScopeAdmin}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "private"},
		rooms: []protocol.Room{
			{
				ID:         "private-floor",
				Visibility: protocol.RoomVisibilityPrivate,
				Members:    []string{"operator"},
			},
		},
	}, policy)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/skill.md", nil)
	request.Header.Set("Authorization", "Bearer admin-secret")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected admin skill, got %d", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "private-floor") {
		t.Fatalf("admin skill should include private room\n%s", body)
	}
}
