package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSelectLeastPrivilegedTokenPicksNarrowest(t *testing.T) {
	tokens := []authn.TokenConfig{
		{ID: "broad", Value: "broad-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin, authn.ScopePair}},
		{ID: "narrow", Value: "narrow-secret", Scopes: []authn.Scope{authn.ScopeWrite}},
		{ID: "unrelated", Value: "unrelated-secret", Scopes: []authn.Scope{authn.ScopePair}},
		{ID: "empty-value", Value: "  ", Scopes: []authn.Scope{authn.ScopeWrite}},
		{ID: "agent-restricted", Value: "agent-secret", Scopes: []authn.Scope{authn.ScopeWrite}, Agents: []string{"alice"}},
	}

	token, ok := selectLeastPrivilegedToken(tokens, authn.ScopeWrite)
	if !ok || token.ID != "narrow" {
		t.Fatalf("selectLeastPrivilegedToken() = %+v, %v, want the narrow write-scoped token", token, ok)
	}
}

func TestSelectLeastPrivilegedTokenNeverUsesAdminOnlyForWrite(t *testing.T) {
	tokens := []authn.TokenConfig{
		{ID: "admin-only", Value: "admin-secret", Scopes: []authn.Scope{authn.ScopeAdmin}},
	}

	if _, ok := selectLeastPrivilegedToken(tokens, authn.ScopeWrite); ok {
		t.Fatal("an admin-only token must never satisfy a write requirement")
	}
}

func TestSelectLeastPrivilegedTokenAdminSatisfiesObserve(t *testing.T) {
	tokens := []authn.TokenConfig{
		{ID: "admin-only", Value: "admin-secret", Scopes: []authn.Scope{authn.ScopeAdmin}},
	}

	token, ok := selectLeastPrivilegedToken(tokens, authn.ScopeObserve)
	if !ok || token.ID != "admin-only" {
		t.Fatalf("expected admin to satisfy an observe requirement as a last resort, got %+v, %v", token, ok)
	}
}

// TestSelectLeastPrivilegedTokenPrefersNonAdminOverShorterAdminList is the
// P1 fix: ranking used to compare scope count alone, so a single-scope
// [admin]-only token beat a single-scope [observe]-only token by config
// order (both have len(Scopes) == 1). An admin-bearing token must never win
// over a non-admin one that satisfies the same requirement, regardless of
// scope count or document order — this checks both orderings.
func TestSelectLeastPrivilegedTokenPrefersNonAdminOverShorterAdminList(t *testing.T) {
	adminFirst := []authn.TokenConfig{
		{ID: "admin-only", Value: "admin-secret", Scopes: []authn.Scope{authn.ScopeAdmin}},
		{ID: "observe-only", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
	}
	if token, ok := selectLeastPrivilegedToken(adminFirst, authn.ScopeObserve); !ok || token.ID != "observe-only" {
		t.Fatalf("selectLeastPrivilegedToken(admin-first) = %+v, %v, want observe-only regardless of order", token, ok)
	}

	observeFirst := []authn.TokenConfig{
		{ID: "observe-only", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
		{ID: "admin-only", Value: "admin-secret", Scopes: []authn.Scope{authn.ScopeAdmin}},
	}
	if token, ok := selectLeastPrivilegedToken(observeFirst, authn.ScopeObserve); !ok || token.ID != "observe-only" {
		t.Fatalf("selectLeastPrivilegedToken(observe-first) = %+v, %v, want observe-only regardless of order", token, ok)
	}
}

// TestSelectLeastPrivilegedTokenPrefersNonAdminOverFewerScopes is the P1
// fix's second case: [write, admin] (2 scopes, admin-bearing) must lose to
// [write, pair] (2 scopes, non-admin) even though scope counts tie — the
// admin/non-admin axis is checked first, scope count only breaks a tie on
// that axis.
func TestSelectLeastPrivilegedTokenPrefersNonAdminOverFewerScopes(t *testing.T) {
	tokens := []authn.TokenConfig{
		{ID: "write-admin", Value: "write-admin-secret", Scopes: []authn.Scope{authn.ScopeWrite, authn.ScopeAdmin}},
		{ID: "write-pair", Value: "write-pair-secret", Scopes: []authn.Scope{authn.ScopeWrite, authn.ScopePair}},
	}

	token, ok := selectLeastPrivilegedToken(tokens, authn.ScopeWrite)
	if !ok || token.ID != "write-pair" {
		t.Fatalf("selectLeastPrivilegedToken() = %+v, %v, want write-pair over the admin-bearing write-admin", token, ok)
	}
}

// TestOperatorFallbackSendPrefersObserveOverAdminOnlyToken is the P1 fix's
// end-to-end regression: `moltnet init --bearer` mints an all-scopes
// "operator" token (which carries admin) and an [observe]-only "console"
// token. A read command (conversations, ScopeObserve) run against that
// canonical config must still pick the narrower console token, not the
// admin-bearing operator token, no matter which one is listed first.
func TestOperatorFallbackSendPrefersObserveOverAdminOnlyToken(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/v1/rooms":
			_ = json.NewEncoder(w).Encode(protocol.RoomPage{})
		case "/v1/dms":
			_ = json.NewEncoder(w).Encode(protocol.DirectConversationPage{})
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	// operator (all scopes, including admin) listed before console
	// (observe-only) — canonical `moltnet init --bearer` document order.
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
		{id: "console", value: "console-secret", scopes: []string{"observe"}},
	})

	if err := run(context.Background(), []string{"conversations"}, "test"); err != nil {
		t.Fatalf("run() conversations error = %v", err)
	}
	if authHeader != "Bearer console-secret" {
		t.Fatalf("expected the narrower observe-only console token, got Authorization header %q", authHeader)
	}
}

// TestOperatorFallbackSendSucceedsInOpenMode is the P2 fix: auth.mode: open
// still authorizes POST /v1/messages on authorizedAnyWithVerifier
// regardless of mode (internal/transport/http.go) — PublicRead's widening
// only ever covers reads (internal/transport/auth.go's
// publicInOpen/optionalAuthInOpen). operatorAuthForScope used to treat any
// non-bearer mode as "no token needed", so a send against an open-mode
// server used to send no Authorization header at all and 401. It must now
// select a token exactly like bearer mode does.
func TestOperatorFallbackSendSucceedsInOpenMode(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if authHeader != "Bearer operator-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
			return
		}
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfigMode(t, directory+"/Moltnet", parsed.Host, "open", []operatorFallbackToken{
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})

	if err := run(context.Background(), []string{"send", "--target", "room:chat", "--text", "hola"}, "test"); err != nil {
		t.Fatalf("run() send error = %v in open mode, want it to succeed with a selected token", err)
	}
	if authHeader != "Bearer operator-secret" {
		t.Fatalf("expected the write-scoped token to be sent in open mode, got Authorization header %q", authHeader)
	}
}

// TestOperatorFallbackSendNoAuthWhenOpenModeHasNoTokens covers
// operatorAuthForScope's remaining no-auth case: auth.mode: open with zero
// configured tokens (valid — unlike bearer, NewPolicy does not require open
// mode to have any) still resolves to no Authorization header, exactly as
// before this fix, rather than failing with a spurious "no scoped token
// found" error.
func TestOperatorFallbackSendNoAuthWhenOpenModeHasNoTokens(t *testing.T) {
	var sawAuthHeader bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuthHeader = r.Header.Get("Authorization") != ""
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfigMode(t, directory+"/Moltnet", parsed.Host, "open", nil)

	if err := run(context.Background(), []string{"send", "--target", "room:chat", "--text", "hola"}, "test"); err != nil {
		t.Fatalf("run() send error = %v, want it to fall back to no auth", err)
	}
	if sawAuthHeader {
		t.Fatal("expected no Authorization header when open mode has no configured tokens")
	}
}

// TestOperatorFallbackConversationsOmitsEmptyMemberID is the P3 fix: the
// zero-setup operator fallback never has a member identity
// (resolveOperatorClient never sets AttachmentConfig.MemberID), so
// `conversations` in fallback mode must omit "member_id" entirely instead
// of printing the fabricated `"member_id": ""`.
func TestOperatorFallbackConversationsOmitsEmptyMemberID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/rooms":
			_ = json.NewEncoder(w).Encode(protocol.RoomPage{})
		case "/v1/dms":
			_ = json.NewEncoder(w).Encode(protocol.DirectConversationPage{})
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"conversations"}, "test"); err != nil {
			t.Fatalf("run() conversations error = %v", err)
		}
	})

	if strings.Contains(output, "member_id") {
		t.Fatalf("expected member_id to be omitted in fallback mode, got %q", output)
	}
}
