package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func TestUIRoutes(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(&fakeService{}, nil)

	t.Run("root redirect", func(t *testing.T) {
		t.Parallel()

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusTemporaryRedirect {
			t.Fatalf("unexpected status %d", response.Code)
		}
		if location := response.Header().Get("Location"); location != "/console/" {
			t.Fatalf("unexpected redirect location %q", location)
		}
	})

	t.Run("console redirect", func(t *testing.T) {
		t.Parallel()

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/console", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusTemporaryRedirect {
			t.Fatalf("unexpected status %d", response.Code)
		}
		if location := response.Header().Get("Location"); location != "/console/" {
			t.Fatalf("unexpected redirect location %q", location)
		}
	})

	t.Run("console serves asset", func(t *testing.T) {
		t.Parallel()

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/console/favicon.svg", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("unexpected status %d", response.Code)
		}
		if body := response.Body.String(); !strings.Contains(body, "<svg") {
			t.Fatalf("expected svg body, got %q", body)
		}
	})
}

func TestUIRoutesWithAccessTokenBootstrap(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		ListenAddr: ":8787",
		Tokens: []authn.TokenConfig{
			{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	handler := NewHTTPHandler(&fakeService{}, policy)

	t.Run("root bootstrap", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/?access_token=observe-secret", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusTemporaryRedirect {
			t.Fatalf("unexpected status %d", response.Code)
		}
		if !strings.Contains(response.Header().Get("Set-Cookie"), authn.CookieName+"=observe-secret") {
			t.Fatalf("expected auth cookie to be set, got %q", response.Header().Get("Set-Cookie"))
		}
	})

	t.Run("console bootstrap", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/console/?access_token=observe-secret", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusTemporaryRedirect {
			t.Fatalf("unexpected status %d", response.Code)
		}
		if response.Header().Get("Location") != "/console/" {
			t.Fatalf("unexpected redirect location %q", response.Header().Get("Location"))
		}
	})

	t.Run("console requires observe auth", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/console/", nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status %d", response.Code)
		}
	})

	t.Run("console serves index with bearer auth", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/console/", nil)
		request.Header.Set("Authorization", "Bearer observe-secret")
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("unexpected status %d", response.Code)
		}
		if !strings.Contains(strings.ToLower(response.Body.String()), "<!doctype html>") {
			t.Fatalf("expected html document, got %q", response.Body.String())
		}
	})
}

func TestUIRoutesOpenModeServesConsolePublicly(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeOpen,
		ListenAddr: ":8787",
		Tokens: []authn.TokenConfig{
			{ID: "writer", Value: "write-secret", Scopes: []authn.Scope{authn.ScopeWrite}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	handler := NewHTTPHandler(&fakeService{}, policy)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/console/", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected public console in open mode, got %d", response.Code)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/console/", nil)
	request.Header.Set("Authorization", "Bearer write-secret")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected under-scoped static token to see public console, got %d", response.Code)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/console/", nil)
	request.Header.Set("Authorization", "Bearer wrong")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid token to be rejected, got %d", response.Code)
	}
}
