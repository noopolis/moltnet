package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestNetworkReportsConsoleSendCapability(t *testing.T) {
	t.Parallel()

	bearerPolicy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		ListenAddr: ":8787",
		Tokens: []authn.TokenConfig{
			{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
			{ID: "writer", Value: "observe-write-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() bearer error = %v", err)
	}

	openPolicy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeOpen,
		ListenAddr: ":8787",
		Tokens: []authn.TokenConfig{
			{ID: "writer", Value: "observe-write-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() open error = %v", err)
	}

	for _, test := range []struct {
		name         string
		policy       *authn.Policy
		token        string
		humanIngress bool
		want         bool
	}{
		{name: "no auth can send when human ingress is enabled", humanIngress: true, want: true},
		{name: "no auth cannot send when human ingress is disabled"},
		{name: "bearer observe only cannot send", policy: bearerPolicy, token: "observe-secret", humanIngress: true},
		{name: "bearer observe write can send", policy: bearerPolicy, token: "observe-write-secret", humanIngress: true, want: true},
		{name: "open anonymous cannot send", policy: openPolicy, humanIngress: true},
		{name: "open observe write can send", policy: openPolicy, token: "observe-write-secret", humanIngress: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			handler := NewHTTPHandler(&fakeService{network: protocol.Network{
				ID:   "local",
				Name: "Local",
				Capabilities: protocol.NetworkCapabilities{
					HumanIngress: test.humanIngress,
				},
			}}, test.policy)
			request := httptest.NewRequest(http.MethodGet, "/v1/network", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d body=%s", response.Code, response.Body.String())
			}

			var network protocol.Network
			if err := json.NewDecoder(response.Body).Decode(&network); err != nil {
				t.Fatalf("decode network response: %v", err)
			}
			if network.Console == nil {
				t.Fatal("expected console capability block")
			}
			if network.Console.CanSendHuman != test.want {
				t.Fatalf("can_send_human = %v, want %v", network.Console.CanSendHuman, test.want)
			}
		})
	}
}

func TestHTTPHandlerAuthScopes(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		ListenAddr: ":8787",
		Tokens: []authn.TokenConfig{
			{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
			{ID: "writer", Value: "write-secret", Scopes: []authn.Scope{authn.ScopeWrite}},
			{ID: "admin", Value: "admin-secret", Scopes: []authn.Scope{authn.ScopeAdmin}},
			{ID: "pair", Value: "pair-secret", Scopes: []authn.Scope{authn.ScopePair}},
			{ID: "attach", Value: "attach-secret", Scopes: []authn.Scope{authn.ScopeAttach}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	handler := NewHTTPHandler(&fakeService{network: protocol.Network{ID: "local", Name: "Local"}}, policy)

	request := httptest.NewRequest(http.MethodGet, "/v1/network", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized network request, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/network", nil)
	request.Header.Set("Authorization", "Bearer observe-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected observe token to read network, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/network", nil)
	request.Header.Set("Authorization", "Bearer attach-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected attach token to read network, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/network", nil)
	request.Header.Set("Authorization", "Bearer pair-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected pair token to read network, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/network", nil)
	request.Header.Set("Authorization", "Bearer write-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected write token to be forbidden for network metadata, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/rooms", nil)
	request.Header.Set("Authorization", "Bearer attach-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected attach token to be forbidden for rooms, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"target":{"kind":"room","room_id":"research"},"from":{"type":"agent","id":"writer"},"parts":[{"kind":"text","text":"hi"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer observe-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected observe token to be forbidden for writes, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"target":{"kind":"room","room_id":"research"},"from":{"type":"agent","id":"writer"},"parts":[{"kind":"text","text":"hi"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer write-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected write token to send message, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/rooms", strings.NewReader(`{"id":"research"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer write-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected write token to be forbidden for rooms admin, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/rooms", strings.NewReader(`{"id":"research"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer admin-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected admin token to create room, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodDelete, "/v1/rooms/research", nil)
	request.Header.Set("Authorization", "Bearer write-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected write token to be forbidden for room removal, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodDelete, "/v1/rooms/research", nil)
	request.Header.Set("Authorization", "Bearer admin-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected admin token to remove room, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodDelete, "/v1/agents/writer", nil)
	request.Header.Set("Authorization", "Bearer write-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected write token to be forbidden for agent removal, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodDelete, "/v1/agents/writer", nil)
	request.Header.Set("Authorization", "Bearer admin-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected admin token to remove agent, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/rooms", nil)
	request.Header.Set("Authorization", "Bearer pair-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected pair token to list rooms, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"target":{"kind":"room","room_id":"research"},"from":{"type":"agent","id":"writer"},"parts":[{"kind":"text","text":"hi"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer pair-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected pair token to relay message, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	request.Header.Set("Authorization", "Bearer pair-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected pair token to be forbidden for event stream, got %d", response.Code)
	}
}

func TestConsoleAccessTokenSetsCookie(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		ListenAddr: ":8787",
		Tokens:     []authn.TokenConfig{{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}}},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	handler := NewHTTPHandler(&fakeService{}, policy)

	request := httptest.NewRequest(http.MethodGet, "/console/?access_token=observe-secret", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect, got %d", response.Code)
	}
	if response.Header().Get("Location") != "/console/" {
		t.Fatalf("unexpected redirect location %q", response.Header().Get("Location"))
	}
	if !strings.Contains(response.Header().Get("Set-Cookie"), authn.CookieName+"=observe-secret") {
		t.Fatalf("expected auth cookie to be set, got %q", response.Header().Get("Set-Cookie"))
	}
}

func TestConsoleRequiresObserveScopeWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		ListenAddr: ":8787",
		Tokens:     []authn.TokenConfig{{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}}},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	handler := NewHTTPHandler(&fakeService{}, policy)

	request := httptest.NewRequest(http.MethodGet, "/console/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized console request, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/console/favicon.svg", nil)
	request.AddCookie(&http.Cookie{Name: authn.CookieName, Value: "observe-secret"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected authorized console asset request, got %d", response.Code)
	}
}
