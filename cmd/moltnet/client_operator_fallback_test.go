package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// operatorFallbackToken names one auth.tokens[] entry for
// writeOperatorFallbackServerConfig.
type operatorFallbackToken struct {
	id     string
	value  string
	scopes []string
}

// writeOperatorFallbackServerConfig writes a Moltnet server config at path
// with server.listen_addr = host, auth.mode: bearer, and the given tokens —
// the same shape writeAdminFallbackServerConfig
// (admin_config_fallback_test.go) uses, reused here for the
// send/read/conversations/participants operator fallback
// (resolveOperatorClient, client_operator_fallback.go).
func writeOperatorFallbackServerConfig(t *testing.T, path string, host string, tokens []operatorFallbackToken) {
	t.Helper()
	writeOperatorFallbackServerConfigMode(t, path, host, authn.ModeBearer, tokens)
}

// writeOperatorFallbackServerConfigMode is writeOperatorFallbackServerConfig
// with an explicit auth.mode, for the P2 "open mode sends no token" fix
// (TestOperatorFallbackSendSucceedsInOpenMode): auth.mode: open takes the
// same tokens shape as bearer.
func writeOperatorFallbackServerConfigMode(t *testing.T, path string, host string, mode string, tokens []operatorFallbackToken) {
	t.Helper()

	var tokenLines strings.Builder
	for _, token := range tokens {
		fmt.Fprintf(&tokenLines, "    - id: %s\n      value: %s\n      scopes: [%s]\n", token.id, token.value, strings.Join(token.scopes, ", "))
	}

	contents := fmt.Sprintf(`version: moltnet.v1

network:
  id: fixture-net
  name: Fixture Moltnet

server:
  listen_addr: %q

auth:
  mode: %s
  tokens:
%s`, host, mode, tokenLines.String())

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func TestOperatorFallbackSendUsesServerConfig(t *testing.T) {
	var requests []*http.Request
	var bodies []protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r)
		if r.URL.Path == "/v1/agents/register" {
			t.Fatal("send in the operator fallback must never call /v1/agents/register")
		}
		var body protocol.SendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true, MessageID: "msg_1", EventID: "evt_1"})
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "console", value: "observe-secret", scopes: []string{"observe"}},
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"send", "--target", "room:chat", "--text", "hola",
		}, "test"); err != nil {
			t.Fatalf("run() send error = %v", err)
		}
	})

	if !strings.Contains(output, `"accepted": true`) {
		t.Fatalf("unexpected send output %q", output)
	}
	if len(requests) != 1 || requests[0].URL.Path != "/v1/messages" {
		t.Fatalf("unexpected requests %#v", requests)
	}
	if got := requests[0].Header.Get("Authorization"); got != "Bearer operator-secret" {
		t.Fatalf("expected the write-scoped operator token, got Authorization header %q", got)
	}
	if bodies[0].From.Type != "human" || bodies[0].From.ID != "operator" {
		t.Fatalf("unexpected sender actor %#v, want the human operator identity", bodies[0].From)
	}
}

func TestOperatorFallbackSendUsesMemberFlagAsIdentity(t *testing.T) {
	var body protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})

	if err := run(context.Background(), []string{
		"send", "--target", "room:chat", "--text", "hola", "--member", "alice",
	}, "test"); err != nil {
		t.Fatalf("run() send error = %v", err)
	}
	if body.From.ID != "alice" {
		t.Fatalf("expected --member to name the sender identity, got %q", body.From.ID)
	}
}

// TestOperatorFallbackSendPrefersWriteScopedOverAdminToken is the
// least-privilege guardrail: when both a narrow write-only token and a
// broader admin+write token exist, send must pick the narrower one, never
// the admin-capable one.
func TestOperatorFallbackSendPrefersWriteScopedOverAdminToken(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "broad", value: "broad-secret", scopes: []string{"observe", "write", "admin", "pair"}},
		{id: "narrow", value: "narrow-secret", scopes: []string{"write"}},
	})

	if err := run(context.Background(), []string{
		"send", "--target", "room:chat", "--text", "hola",
	}, "test"); err != nil {
		t.Fatalf("run() send error = %v", err)
	}
	if authHeader != "Bearer narrow-secret" {
		t.Fatalf("expected the narrower write-only token, got Authorization header %q", authHeader)
	}
}

// TestOperatorFallbackReadNeverUsesAdminOnlyTokenWhenObserveExists mirrors
// the write case for reads: an observe-only token must win over a broader
// admin token when both satisfy the read.
func TestOperatorFallbackReadNeverUsesAdminOnlyTokenWhenObserveExists(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(protocol.MessagePage{Messages: []protocol.Message{{ID: "msg_1"}}})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "broad", value: "broad-secret", scopes: []string{"observe", "write", "admin", "pair"}},
		{id: "narrow", value: "narrow-secret", scopes: []string{"observe"}},
	})

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"read", "--target", "room:chat"}, "test"); err != nil {
			t.Fatalf("run() read error = %v", err)
		}
	})

	if authHeader != "Bearer narrow-secret" {
		t.Fatalf("expected the narrower observe-only token, got Authorization header %q", authHeader)
	}
	if !strings.Contains(output, "msg_1") {
		t.Fatalf("unexpected read output %q", output)
	}
}

// TestOperatorFallbackRefusesNonLoopbackListenAddr is the second security
// guardrail: a server bound to a non-loopback host must never have its
// operator token dialed automatically. The error must name the explicit
// --base-url/--token-env alternative.
func TestOperatorFallbackRefusesNonLoopbackListenAddr(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", "192.168.1.50:8787", []operatorFallbackToken{
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})

	err := run(context.Background(), []string{"send", "--target", "room:chat", "--text", "hola"}, "test")
	if err == nil {
		t.Fatal("expected the non-loopback fallback to be refused")
	}
	if !strings.Contains(err.Error(), "--base-url") || !strings.Contains(err.Error(), "--token-env") {
		t.Fatalf("expected the error to name --base-url and --token-env, got %v", err)
	}
	if strings.Contains(err.Error(), "operator-secret") {
		t.Fatal("token value must never appear in an error message")
	}
}

// TestOperatorOverrideBaseURLSkipsFallback confirms an explicit --base-url
// (with --token-env) always wins outright and never consults the local
// server config, mirroring admin's existing --base-url precedence.
func TestOperatorOverrideBaseURLSkipsFallback(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()

	t.Setenv("MOLTNET_TEST_TOKEN", "env-secret")
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	// A server config sits right here in cwd too, at a non-loopback host
	// that would otherwise be refused outright — proving --base-url never
	// even looks at it.
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", "192.168.1.50:8787", nil)

	if err := run(context.Background(), []string{
		"send", "--target", "room:chat", "--text", "hola",
		"--base-url", server.URL, "--token-env", "MOLTNET_TEST_TOKEN",
	}, "test"); err != nil {
		t.Fatalf("run() send error = %v", err)
	}
	if authHeader != "Bearer env-secret" {
		t.Fatalf("expected the --token-env token, got Authorization header %q", authHeader)
	}
}

func TestOperatorFallbackConversationsShowsAllVisibleRooms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/rooms":
			_ = json.NewEncoder(w).Encode(protocol.RoomPage{Rooms: []protocol.Room{{ID: "general"}, {ID: "random"}}})
		case "/v1/dms":
			_ = json.NewEncoder(w).Encode(protocol.DirectConversationPage{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
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

	// Unlike the agent-config path (TestRunConversationsFiltersToAttachedRooms),
	// there is no client-side rooms allowlist here — both rooms the token
	// can see should show, not be silently filtered to zero.
	if !strings.Contains(output, `"general"`) || !strings.Contains(output, `"random"`) {
		t.Fatalf("expected both rooms in fallback mode, got %q", output)
	}
}

func TestSendPositionalArgumentForms(t *testing.T) {
	var body protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	cases := []struct {
		name string
		args []string
	}{
		{"bare positional target and text", []string{"room:chat", "hola"}},
		{"flagged target, positional text", []string{"--target", "room:chat", "hola"}},
		// Go's flag package stops parsing at the first non-flag argument, so
		// a flag can never follow a positional one — this is the flags-first
		// analogue of the "flagged target, positional text" case above, and
		// also the exact form the field session that motivated this fix
		// tried (`moltnet send -text hola room:chat`).
		{"flagged text, positional target", []string{"--text", "hola", "room:chat"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body = protocol.SendMessageRequest{}
			directory := t.TempDir()
			t.Chdir(directory)
			t.Setenv("HOME", t.TempDir())
			writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
				{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
			})

			args := append([]string{"send"}, testCase.args...)
			if err := run(context.Background(), args, "test"); err != nil {
				t.Fatalf("run(%v) error = %v", args, err)
			}
			if body.Target.RoomID != "chat" || body.Parts[0].Text != "hola" {
				t.Fatalf("run(%v): unexpected request body %#v", args, body)
			}
		})
	}
}

func TestReadPositionalTargetForm(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(protocol.MessagePage{Messages: []protocol.Message{{ID: "msg_1"}}})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})

	if err := run(context.Background(), []string{"read", "room:chat"}, "test"); err != nil {
		t.Fatalf("run() read room:chat error = %v", err)
	}
	if requestedPath != "/v1/rooms/chat/messages" {
		t.Fatalf("unexpected requested path %q", requestedPath)
	}
}

// selectLeastPrivilegedToken's own unit tests live in
// operator_fallback_privilege_test.go, alongside the P1 admin-vs-scope-count
// ranking fix.
