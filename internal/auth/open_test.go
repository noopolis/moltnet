package auth

import (
	"strings"
	"testing"
)

func TestOpenPolicyAcceptsOptionalStaticTokens(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{Mode: ModeOpen})
	if err != nil {
		t.Fatalf("NewPolicy(open) error = %v", err)
	}
	if !policy.Enabled() || !policy.Open() || policy.Bearer() || policy.None() {
		t.Fatalf("unexpected open policy helpers")
	}

	policy, err = NewPolicy(Config{
		Mode: ModeOpen,
		Tokens: []TokenConfig{{
			ID:     "operator",
			Value:  "secret",
			Scopes: []Scope{ScopeObserve, ScopeAdmin},
		}},
	})
	if err != nil {
		t.Fatalf("NewPolicy(open with token) error = %v", err)
	}
	claims, ok := policy.StaticClaimsForToken("secret")
	if !ok || claims.CredentialKey != "token:operator" || !claims.Allows(ScopeAdmin) {
		t.Fatalf("unexpected static claims %#v ok=%v", claims, ok)
	}
}

func TestAgentTokenHelpers(t *testing.T) {
	t.Parallel()

	token, err := GenerateAgentToken()
	if err != nil {
		t.Fatalf("GenerateAgentToken() error = %v", err)
	}
	if !strings.HasPrefix(token, AgentTokenPrefix) {
		t.Fatalf("expected token prefix, got %q", token)
	}
	encoded := strings.TrimPrefix(token, AgentTokenPrefix)
	if len(encoded) < 43 {
		t.Fatalf("expected at least 32 bytes of url-safe entropy, got encoded length %d", len(encoded))
	}
	if strings.ContainsAny(encoded, "+/=") {
		t.Fatalf("expected raw URL-safe token encoding, got %q", encoded)
	}

	key := AgentTokenCredentialKey(token)
	if !strings.HasPrefix(key, "agent-token:") || strings.Contains(key, token) {
		t.Fatalf("unexpected credential key %q", key)
	}

	claims := NewAgentTokenClaims("luna", key)
	if !claims.AgentToken() || claims.StaticToken() || !claims.Allows(ScopeWrite) || !claims.Allows(ScopeAttach) {
		t.Fatalf("unexpected agent token claims %#v", claims)
	}
	if claims.Allows(ScopeAdmin) || claims.Allows(ScopeObserve) || claims.Allows(ScopePair) {
		t.Fatalf("agent token claims granted elevated scope %#v", claims)
	}
	if !claims.AllowsAgent("luna") || claims.AllowsAgent("atlas") {
		t.Fatalf("agent token claims should be restricted to own agent")
	}
}
