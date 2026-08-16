package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
)

// --- consoleBaseURL: URL derivation table --------------------------------
//
// Split out of console_test.go (which was approaching the 400-line repo
// limit) purely to make room there for runConsole behavior cases; this file
// owns everything about deriving/validating URLs the console dials or
// opens, nothing about runConsole's own flag/health/browser dispatch.

func TestConsoleBaseURL(t *testing.T) {
	cases := []struct {
		name       string
		listenAddr string
		want       string
	}{
		{"wildcard colon-port", ":8787", "http://127.0.0.1:8787"},
		{"explicit loopback host", "127.0.0.1:9000", "http://127.0.0.1:9000"},
		{"explicit wildcard host", "0.0.0.0:8787", "http://127.0.0.1:8787"},
		{"explicit non-loopback host", "moltnet.example.com:8787", "http://moltnet.example.com:8787"},
		{"ipv6 wildcard short form", "[::]:8787", "http://127.0.0.1:8787"},
		{"ipv6 wildcard expanded form", "[0:0:0:0:0:0:0:0]:8787", "http://127.0.0.1:8787"},
		{"explicit ipv6 loopback", "[::1]:8787", "http://[::1]:8787"},
		{"empty falls back to default", "", "http://127.0.0.1:8787"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := consoleBaseURL(testCase.listenAddr)
			if err != nil {
				t.Fatalf("consoleBaseURL(%q) error = %v", testCase.listenAddr, err)
			}
			if got != testCase.want {
				t.Fatalf("consoleBaseURL(%q) = %q, want %q", testCase.listenAddr, got, testCase.want)
			}
		})
	}
}

func TestConsoleBaseURLInvalid(t *testing.T) {
	if _, err := consoleBaseURL("not-a-valid-addr"); err == nil {
		t.Fatal("expected an error for an address with no host:port separator")
	}
}

// --- defaultOpenURL: scheme/host guard --------------------------------
//
// This only exercises the guard itself (a return before exec.LookPath is
// ever reached), never the real "open"/"xdg-open" exec below it -- a
// non-http scheme must fail before this package would spawn any process,
// let alone a real browser.

func TestDefaultOpenURLRejectsNonHTTPURL(t *testing.T) {
	cases := []string{
		"https://127.0.0.1:8787/console/",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"not a url at all\x00",
		"http://",
	}

	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			if err := defaultOpenURL(url); err == nil {
				t.Fatalf("defaultOpenURL(%q) = nil, want an error rejecting the non-http URL before ever exec'ing an opener", url)
			}
		})
	}
}

// --- browser-open access_token augmentation --------------------------------
//
// consoleObserveToken/runConsole's browser-open path appends
// ?access_token=<token> only when it would otherwise open the browser
// straight onto a 401 (internal/transport/auth.go's authorizedConsole,
// bearer mode, no public_read) and a config token can back it -- and never
// where a script could capture it (--print, --no-open, non-TTY all use the
// scaffolding in console_test.go and are covered there already).

// writeConsoleBearerTestConfig writes a Moltnet config for networkID, in
// auth.mode bearer, with exactly one auth.tokens[] entry carrying scopes.
func writeConsoleBearerTestConfig(t *testing.T, path, networkID, listenAddr, tokenValue string, scopes []authn.Scope) {
	t.Helper()
	scopeNames := make([]string, len(scopes))
	for i, scope := range scopes {
		scopeNames[i] = string(scope)
	}
	body := fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: %q
  human_ingress: true
  debug_events: false

storage:
  kind: sqlite
  sqlite:
    path: .moltnet/moltnet.db

auth:
  mode: %s
  tokens:
    - id: test-token
      value: %q
      scopes: [%s]

rooms: []
pairings: []
`, networkID, networkID+" Moltnet", listenAddr, authn.ModeBearer, tokenValue, strings.Join(scopeNames, ", "))
	writeConsoleRawTestConfig(t, path, body)
}

// writeConsoleOpenModeTestConfig writes a Moltnet config for networkID in
// auth.mode open, which anonymousAllowedInOpen/authorizedConsole
// (internal/transport/auth.go) already let an unauthenticated request
// through, so runConsole must never append a token for it.
func writeConsoleOpenModeTestConfig(t *testing.T, path, networkID, listenAddr string) {
	t.Helper()
	body := fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: %q
  human_ingress: true
  debug_events: false

storage:
  kind: sqlite
  sqlite:
    path: .moltnet/moltnet.db

auth:
  mode: %s

rooms: []
pairings: []
`, networkID, networkID+" Moltnet", listenAddr, authn.ModeOpen)
	writeConsoleRawTestConfig(t, path, body)
}

func writeConsoleRawTestConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// TestRunConsoleBearerNetworkWithPrivilegedTokenNeverAppendsIt is the P1
// security regression test: a token whose scopes merely INCLUDE "observe"
// alongside a privileged scope (here "admin") must never be selected by
// consoleObserveToken. An earlier version of this test (then named
// TestRunConsoleBearerNetworkOpensURLWithObserveToken) encoded the wrong
// contract by asserting the opposite — that this exact [observe, admin]
// config's token WAS appended — which is precisely the P1: the server
// copies ?access_token= verbatim into an HttpOnly cookie with no scope
// downgrade (maybeSetConsoleAuthCookie, internal/transport/auth.go), so
// handing the browser this token would open an admin-capable console
// session. Fixed: no matching token exists (none of this config's tokens
// carry exactly [observe]), so runConsole must fall back to the plain URL
// plus the same missing-observe-token note as
// TestRunConsoleBearerNetworkNoObserveTokenOpensPlainURLWithNote below —
// never fabricate or widen the search to a privileged token instead.
func TestRunConsoleBearerNetworkWithPrivilegedTokenNeverAppendsIt(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "jill-net", listenAddrOf(t, server), "jill-observe-secret", []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin})

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 1 || (*opened)[0] != wantURL {
		t.Fatalf("expected exactly one plain browser open of %q (no token appended for a privileged-scope token), got %v", wantURL, *opened)
	}
	if strings.Contains(output, "jill-observe-secret") {
		t.Fatalf("expected the printed output to never contain the privileged token, got %q", output)
	}
	if !strings.Contains(output, "note") || !strings.Contains(output, "observe") {
		t.Fatalf("expected the missing-observe-only-token note, got %q", output)
	}
}

// TestRunConsoleBearerNetworkOpensURLWithObserveOnlyToken covers the one
// case consoleObserveToken must still say yes to: a token whose scopes are
// EXACTLY [observe], nothing more. That, and only that, is safe to hand the
// browser as ?access_token=.
func TestRunConsoleBearerNetworkOpensURLWithObserveOnlyToken(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "jill-net", listenAddrOf(t, server), "jill-observe-only-secret", []authn.Scope{authn.ScopeObserve})

	wantBaseURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	wantOpenedURL := wantBaseURL + "?access_token=jill-observe-only-secret"
	if len(*opened) != 1 || (*opened)[0] != wantOpenedURL {
		t.Fatalf("expected exactly one browser open of %q, got %v", wantOpenedURL, *opened)
	}
	// The printed ready line stays token-free: only openURLFunc ever saw
	// the augmented URL above.
	if strings.Contains(output, "jill-observe-only-secret") {
		t.Fatalf("expected the printed output to never contain the access token, got %q", output)
	}
	if !strings.Contains(output, wantBaseURL) {
		t.Fatalf("expected the ready line to still name the bare console URL, got %q", output)
	}
}

func TestRunConsoleBearerNetworkPrintOmitsAccessToken(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "kim-net", listenAddrOf(t, server), "kim-observe-secret", []authn.Scope{authn.ScopeObserve})

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath, "--print"}, "test"); err != nil {
			t.Fatalf("console --print error = %v", err)
		}
	})

	if strings.TrimSpace(output) != wantURL {
		t.Fatalf("output = %q, want exactly %q (no access_token under --print)", output, wantURL)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open attempt under --print, got %v", *opened)
	}
}

func TestRunConsoleOpenModeNetworkNeverAppendsAccessToken(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleOpenModeTestConfig(t, configPath, "leo-net", listenAddrOf(t, server))

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 1 || (*opened)[0] != wantURL {
		t.Fatalf("expected exactly one plain browser open of %q on an open-mode network, got %v", wantURL, *opened)
	}
	if !strings.Contains(output, wantURL) {
		t.Fatalf("expected the ready line to name the console URL, got %q", output)
	}
}

func TestRunConsoleBearerNetworkNoObserveTokenOpensPlainURLWithNote(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	// The only token in this config carries "write", never "observe" -- no
	// token here can legitimately view the console, so runConsole must not
	// fabricate one.
	writeConsoleBearerTestConfig(t, configPath, "mona-net", listenAddrOf(t, server), "mona-write-secret", []authn.Scope{authn.ScopeWrite})

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 1 || (*opened)[0] != wantURL {
		t.Fatalf("expected exactly one plain browser open of %q, got %v", wantURL, *opened)
	}
	if strings.Contains(output, "mona-write-secret") {
		t.Fatalf("expected the printed output to never contain the write-scoped token, got %q", output)
	}
	if !strings.Contains(output, "note") || !strings.Contains(output, "observe") {
		t.Fatalf("expected a one-line note about the missing observe-scoped token, got %q", output)
	}
}
