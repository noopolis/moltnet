package auth

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewPolicyAndAuthenticateRequest(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{
		Mode:       ModeBearer,
		ListenAddr: ":8787",
		Tokens: []TokenConfig{
			{
				ID:     "operator",
				Value:  "secret",
				Scopes: []Scope{ScopeObserve, ScopeWrite},
				Agents: []string{"researcher"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost/v1/network", nil)
	request.Header.Set("Authorization", "Bearer secret")
	claims, err := policy.AuthenticateRequest(request, ScopeObserve)
	if err != nil {
		t.Fatalf("AuthenticateRequest() error = %v", err)
	}
	if !claims.Allows(ScopeObserve) || !claims.AllowsAgent("researcher") || claims.AllowsAgent("writer") {
		t.Fatalf("unexpected claims %#v", claims)
	}
}

func TestAuthenticateRequestErrors(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{
		Mode:   ModeBearer,
		Tokens: []TokenConfig{{Value: "secret", Scopes: []Scope{ScopeObserve}}},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost/v1/network", nil)
	if _, err := policy.AuthenticateRequest(request, ScopeObserve); err == nil {
		t.Fatal("expected missing auth error")
	}

	request.Header.Set("Authorization", "Bearer wrong")
	if _, err := policy.AuthenticateRequest(request, ScopeObserve); err == nil {
		t.Fatal("expected invalid token error")
	}

	request.Header.Set("Authorization", "Bearer secret")
	if _, err := policy.AuthenticateRequest(request, ScopeAdmin); err == nil {
		t.Fatal("expected forbidden scope error")
	}
}

func TestPolicyAllowsCookieAndScopedQueryTokens(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{
		Mode:   ModeBearer,
		Tokens: []TokenConfig{{Value: "secret", Scopes: []Scope{ScopeObserve}}},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost/console/?access_token=secret", nil)
	if _, err := policy.AuthenticateRequest(request, ScopeObserve); err != nil {
		t.Fatalf("expected query token auth, got %v", err)
	}

	request = httptest.NewRequest(http.MethodGet, "http://localhost/v1/network?access_token=secret", nil)
	if _, err := policy.AuthenticateRequest(request, ScopeObserve); err == nil {
		t.Fatal("expected query token to be rejected outside console and stream routes")
	}

	request = httptest.NewRequest(http.MethodGet, "http://localhost/v1/network", nil)
	request.AddCookie(&http.Cookie{Name: CookieName, Value: "secret"})
	if _, err := policy.AuthenticateRequest(request, ScopeObserve); err != nil {
		t.Fatalf("expected cookie token auth, got %v", err)
	}
}

func TestCheckOrigin(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{ListenAddr: ":8787"})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost/v1/attach", nil)
	request.Header.Set("Origin", "http://localhost:8787")
	if !policy.CheckOrigin(request) {
		t.Fatal("expected localhost origin to be allowed")
	}

	request.Header.Set("Origin", "https://evil.example")
	if policy.CheckOrigin(request) {
		t.Fatal("expected foreign origin to be denied")
	}

	request.Header.Del("Origin")
	if !policy.CheckOrigin(request) {
		t.Fatal("expected requests without origin header to be allowed")
	}
}

func TestAllowsQueryAccessToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: "/console/", want: true},
		{path: "/console/app.js", want: true},
		{path: "/v1/events/stream", want: false},
		{path: "/v1/network", want: false},
	}

	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, "http://localhost"+test.path, nil)
		if got := allowsQueryAccessToken(request); got != test.want {
			t.Fatalf("allowsQueryAccessToken(%q) = %v, want %v", test.path, got, test.want)
		}
	}
}

func TestPolicyHelpers(t *testing.T) {
	t.Parallel()

	if _, err := NewPolicy(Config{Mode: "wat"}); err == nil {
		t.Fatal("expected unsupported auth mode error")
	}
	if _, err := NewPolicy(Config{Mode: ModeBearer}); err == nil {
		t.Fatal("expected missing bearer token error")
	}
	if _, err := NewPolicy(Config{Mode: ModeBearer, Tokens: []TokenConfig{{Value: "secret"}}}); err == nil {
		t.Fatal("expected missing scope error")
	}
	if _, err := NewPolicy(Config{Mode: ModeBearer, Tokens: []TokenConfig{{Value: "secret", Scopes: []Scope{"wat"}}}}); err == nil {
		t.Fatal("expected unsupported scope error")
	}

	ctx := WithClaims(context.Background(), Claims{TokenID: "operator"})
	claims, ok := ClaimsFromContext(ctx)
	if !ok || claims.TokenID != "operator" {
		t.Fatalf("unexpected claims from context %#v %v", claims, ok)
	}

	if _, ok := ClaimsFromContext(context.Background()); ok {
		t.Fatal("expected missing claims in empty context")
	}
}

func TestNewPolicyDerivesStableTokenIDsWhenUnset(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{
		Mode:   ModeBearer,
		Tokens: []TokenConfig{{Value: "secret", Scopes: []Scope{ScopeObserve}}},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost/v1/network", nil)
	request.Header.Set("Authorization", "Bearer secret")
	claims, err := policy.AuthenticateRequest(request, ScopeObserve)
	if err != nil {
		t.Fatalf("AuthenticateRequest() error = %v", err)
	}
	if claims.TokenID == "" {
		t.Fatal("expected derived token id")
	}
}

func TestPolicyHelperBranches(t *testing.T) {
	t.Parallel()

	authErr := &Error{Status: http.StatusUnauthorized, Message: "nope"}
	if got := authErr.Error(); got != "nope" {
		t.Fatalf("Error() = %q", got)
	}

	var empty Claims
	if empty.Allows(ScopeObserve) {
		t.Fatal("expected empty claims to deny scopes")
	}
	if !empty.AllowsAgent("anyone") {
		t.Fatal("expected empty claims to allow any agent")
	}

	restricted := newClaims(TokenConfig{
		Scopes: []Scope{ScopeObserve},
		Agents: []string{" alpha "},
	})
	if !restricted.Allows(ScopeObserve) {
		t.Fatal("expected observe scope to be allowed")
	}
	if restricted.Allows(ScopeWrite) {
		t.Fatal("expected write scope to be denied")
	}
	if !restricted.AllowsAgent("alpha") || restricted.AllowsAgent("beta") {
		t.Fatal("expected agent restriction to be enforced")
	}
}

func TestPolicyIsSecureRequest(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{
		Mode:                ModeBearer,
		TrustForwardedProto: true,
		Tokens:              []TokenConfig{{Value: "secret", Scopes: []Scope{ScopeObserve}}},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost/console/", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	if !policy.IsSecureRequest(request) {
		t.Fatal("expected trusted forwarded proto to mark request secure")
	}

	policy, err = NewPolicy(Config{
		Mode:   ModeBearer,
		Tokens: []TokenConfig{{Value: "secret", Scopes: []Scope{ScopeObserve}}},
	})
	if err != nil {
		t.Fatalf("NewPolicy() default error = %v", err)
	}
	if policy.IsSecureRequest(request) {
		t.Fatal("expected forwarded proto to be ignored unless explicitly trusted")
	}

	tlsRequest := httptest.NewRequest(http.MethodGet, "https://localhost/console/", nil)
	tlsRequest.TLS = &tls.ConnectionState{}
	if !policy.IsSecureRequest(tlsRequest) {
		t.Fatal("expected direct TLS request to be secure")
	}
}

func TestOriginNormalizationHelpers(t *testing.T) {
	t.Parallel()

	if got, ok := normalizeOrigin("https://example.com/path"); ok || got != "" {
		t.Fatalf("expected origin with path to be rejected, got %q %v", got, ok)
	}
	if got, ok := normalizeOrigin("://bad"); ok || got != "" {
		t.Fatalf("expected malformed origin to be rejected, got %q %v", got, ok)
	}

	origins := normalizeOrigins([]string{"https://example.com", "bad-origin"}, ":8787")
	if len(origins) != 1 || origins[0] != "https://example.com" {
		t.Fatalf("unexpected normalized origins %#v", origins)
	}

	defaulted := normalizeOrigins(nil, "127.0.0.1:8787")
	if len(defaulted) == 0 {
		t.Fatal("expected default origins from listen addr")
	}
}
