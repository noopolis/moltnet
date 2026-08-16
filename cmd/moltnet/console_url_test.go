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

// The old TestRunConsoleBearerNetworkWithPrivilegedTokenNeverAppendsIt (a
// bearer network whose only token carried more than [observe], e.g.
// [observe, admin]) and TestRunConsoleBearerNetworkNoObserveTokenOpensPlainURLWithNote
// (a bearer network whose only token carried [write]) both moved to
// console_selfheal_test.go: `moltnet console` no longer just prints a note
// in either case, it self-heals by minting and writing a fresh
// observe-only token — the P1 security invariant they pin (a
// privileged-scoped token must never be the one appended to the browser
// URL) still holds and is still asserted there, alongside the new
// self-heal behavior.

// TestRunConsoleBearerNetworkOpensURLWithObserveOnlyToken covers the one
// case consoleObserveToken must still say yes to: a token whose scopes are
// EXACTLY [observe], nothing more. That, and only that, is safe to hand the
// browser as ?access_token=.
func TestRunConsoleBearerNetworkOpensURLWithObserveOnlyToken(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	// probeConsoleToken (the P1 fix) must see the token accepted before
	// runConsole ever appends it -- consoleTestServer's /v1/rooms answers 200
	// so this exercises the "already works" branch, not the down-server one.
	server := consoleTestServer(t, http.StatusOK, http.StatusOK)
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

// TestRunConsoleBearerNetworkObserveOnlyTokenNotYetLoadedGivesActionableLine
// is the P1 field bug's "run 2" shape: an observe-only token already sits in
// the config (written by an earlier run, by init --bearer, or by hand), but
// the live server has never actually loaded it (consoleTestServer's
// /v1/rooms answers 401, simulating a server that has not reloaded
// auth.tokens[] since this token was written). Before this fix, runConsole
// appended it to the browser URL unconditionally on the strength of the
// config file alone -- opening a 307-then-401 behind a bare green
// checkmark. Now: no browser open at all, and the one actionable next
// command printed instead, exit 0 (nothing this command itself attempted
// failed).
func TestRunConsoleBearerNetworkObserveOnlyTokenNotYetLoadedGivesActionableLine(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := consoleTestServer(t, http.StatusOK, http.StatusUnauthorized)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "morgan-net", listenAddrOf(t, server), "morgan-observe-only-secret", []authn.Scope{authn.ScopeObserve})

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 0 {
		t.Fatalf("expected no browser open when the live server has not loaded the token, got %v", *opened)
	}
	if !strings.Contains(output, "has not loaded it yet") {
		t.Fatalf("expected the token-not-loaded note, got %q", output)
	}
	if !strings.Contains(output, "moltnet start") {
		t.Fatalf("expected the one exact command to load the token, got %q", output)
	}
	if strings.Contains(output, "morgan-observe-only-secret") {
		t.Fatalf("expected the token to never be printed, got %q", output)
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
