package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

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

	request = httptest.NewRequest(http.MethodGet, "/console/app.js", nil)
	request.AddCookie(&http.Cookie{Name: authn.CookieName, Value: "observe-secret"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected authorized console asset request, got %d", response.Code)
	}
}
