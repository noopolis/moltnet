package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func TestAuthorizedAnyWithoutPolicyReturnsNext(t *testing.T) {
	t.Parallel()

	called := false
	handler := authorizedAny(nil, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		called = true
		response.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/network", nil)
	response := httptest.NewRecorder()
	handler(response, request)

	if !called {
		t.Fatal("expected wrapped handler to be called")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("unexpected status %d", response.Code)
	}
}

func TestAuthenticateAnySelectsMatchingScope(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		ListenAddr: ":8787",
		Tokens: []authn.TokenConfig{
			{ID: "writer", Value: "write-secret", Scopes: []authn.Scope{authn.ScopeWrite}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	request.Header.Set("Authorization", "Bearer write-secret")

	claims, err := authenticateAny(policy, request, []authn.Scope{authn.ScopeObserve, authn.ScopeWrite})
	if err != nil {
		t.Fatalf("authenticateAny() error = %v", err)
	}
	if claims.TokenID != "writer" {
		t.Fatalf("unexpected claims %#v", claims)
	}
}

func TestMaybeSetConsoleAuthCookie(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:                authn.ModeBearer,
		ListenAddr:          ":8787",
		TrustForwardedProto: true,
		Tokens:              []authn.TokenConfig{{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}}},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/console/?access_token=observe-secret&view=live", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()

	if !maybeSetConsoleAuthCookie(policy, response, request) {
		t.Fatal("expected console auth cookie flow to trigger")
	}
	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("unexpected status %d", response.Code)
	}
	if location := response.Header().Get("Location"); location != "/console/?view=live" {
		t.Fatalf("unexpected redirect location %q", location)
	}

	cookie := response.Result().Cookies()
	if len(cookie) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookie))
	}
	if !cookie[0].Secure || cookie[0].Value != "observe-secret" {
		t.Fatalf("unexpected cookie %#v", cookie[0])
	}

	request = httptest.NewRequest(http.MethodGet, "/console/", nil)
	response = httptest.NewRecorder()
	if maybeSetConsoleAuthCookie(policy, response, request) {
		t.Fatal("expected no-op without access token")
	}
}
