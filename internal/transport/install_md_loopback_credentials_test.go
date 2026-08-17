package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestInstallMarkdownLoopbackHonorsValidCredentials is the P2 regression
// test (PLAN.md phase 6c review): installMarkdownRoute's loopback carve-out
// (discovery.go) used to take the anonymous branch unconditionally on any
// direct-loopback request, so an operator token presented on loopback
// rendered "Readable rooms: none declared" — the same rendering an
// unauthenticated caller got — while the identical token routed through a
// reverse proxy (the non-loopback `gated` path) rendered the full room
// list. That's fail-closed but clearly unintended: a same-machine operator
// with a real credential should see at least what a proxied caller with the
// same credential sees. This proves the loopback render with a valid
// operator token now matches the non-loopback authenticated render.
func TestInstallMarkdownLoopbackHonorsValidCredentials(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{
			{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local_lab", Name: "Local Lab"},
		rooms: []protocol.Room{
			{ID: "lobby", Visibility: protocol.RoomVisibilityPublic},
			{ID: "secret-ops", Visibility: protocol.RoomVisibilityPrivate},
		},
	}, policy)

	// Ground truth: the operator token via a non-loopback (proxied) request
	// sees both rooms.
	proxied := httptest.NewRequest(http.MethodGet, "/install.md", nil)
	proxied.Header.Set("Authorization", "Bearer operator-secret")
	proxiedResponse := httptest.NewRecorder()
	handler.ServeHTTP(proxiedResponse, proxied)
	if proxiedResponse.Code != http.StatusOK {
		t.Fatalf("expected proxied authenticated GET /install.md to serve, got %d: %s", proxiedResponse.Code, proxiedResponse.Body.String())
	}
	proxiedBody := proxiedResponse.Body.String()
	for _, want := range []string{"lobby", "secret-ops"} {
		if !strings.Contains(proxiedBody, want) {
			t.Fatalf("proxied authenticated /install.md missing %q:\n%s", want, proxiedBody)
		}
	}

	// The same token, presented on a direct-loopback request, must render
	// the same room list — not the anonymous "none declared" fallback.
	loopback := loopbackRequest("/install.md")
	loopback.Header.Set("Authorization", "Bearer operator-secret")
	loopbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(loopbackResponse, loopback)
	if loopbackResponse.Code != http.StatusOK {
		t.Fatalf("expected loopback authenticated GET /install.md to serve, got %d: %s", loopbackResponse.Code, loopbackResponse.Body.String())
	}
	loopbackBody := loopbackResponse.Body.String()
	for _, want := range []string{"lobby", "secret-ops"} {
		if !strings.Contains(loopbackBody, want) {
			t.Fatalf("loopback authenticated /install.md missing %q (should match the proxied render):\n%s", want, loopbackBody)
		}
	}
	if strings.Contains(loopbackBody, "none declared") {
		t.Fatalf("loopback authenticated /install.md should not fall back to the anonymous render:\n%s", loopbackBody)
	}
}

// TestInstallMarkdownLoopbackFallsBackOnInvalidCredentials covers the other
// half of the P2 fix: a direct-loopback request carrying a bearer token
// that does not match any configured credential must still fall back to
// the anonymous render (same as no credentials at all), not 401 — the
// carve-out's whole point is that a same-machine caller need not
// authenticate, so a garbled or stale token should degrade gracefully
// rather than lock the caller out of the join guide entirely.
func TestInstallMarkdownLoopbackFallsBackOnInvalidCredentials(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		PublicRead: true,
		Tokens: []authn.TokenConfig{
			{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local_lab", Name: "Local Lab"},
		rooms: []protocol.Room{
			{ID: "lobby", Visibility: protocol.RoomVisibilityPublic},
			{ID: "secret-ops", Visibility: protocol.RoomVisibilityPrivate},
		},
	}, policy)

	loopback := loopbackRequest("/install.md")
	loopback.Header.Set("Authorization", "Bearer not-a-real-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, loopback)
	if response.Code != http.StatusOK {
		t.Fatalf("expected loopback GET /install.md with an invalid token to still serve (anonymous fallback), got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "secret-ops") {
		t.Fatalf("loopback /install.md with an invalid token leaked the private room \"secret-ops\":\n%s", body)
	}
	if !strings.Contains(body, "lobby") {
		t.Fatalf("loopback /install.md with an invalid token should still show the public room via the anonymous fallback:\n%s", body)
	}
}
