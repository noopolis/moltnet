package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/service"
)

// This file covers `moltnet console`'s self-heal path (console_selfheal.go):
// a bearer-mode network with no token scoped to exactly [observe] used to
// leave the operator stuck on a raw 401 with no in-product way forward
// (consoleObserveToken refuses to hand the browser anything more
// privileged, by design — see console_url_test.go's P1 history). Self-heal
// closes that gap by minting a fresh observe-only token, writing it, and
// restarting the managed service (when one exists) so it actually takes
// effect before ever putting it in the browser URL.

// writeConsoleUnitFile marks networkID's managed service as installed by
// writing a real (placeholder-content) unit file at the path
// service.IsInstalled checks — the same setup console_down_test.go's
// service-installed cases use — so restartForConsoleToken's IsInstalled
// check finds it and proceeds to call the (fake) Restart.
func writeConsoleUnitFile(t *testing.T, networkID string) {
	t.Helper()
	unitPath, err := service.SystemdUnitPath(networkID)
	if err != nil {
		t.Fatalf("SystemdUnitPath(%q): %v", networkID, err)
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(unitPath), err)
	}
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o600); err != nil {
		t.Fatalf("write %q: %v", unitPath, err)
	}
}

// onlyObserveOnlyToken returns the value of the one auth.tokens[] entry in
// cfg scoped to exactly [observe], failing the test if there is not exactly
// one.
func onlyObserveOnlyToken(t *testing.T, cfg app.Config) authn.TokenConfig {
	t.Helper()
	var found []authn.TokenConfig
	for _, token := range cfg.Auth.Tokens {
		if len(token.Scopes) == 1 && token.Scopes[0] == authn.ScopeObserve {
			found = append(found, token)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one observe-only token, got %d: %+v", len(found), cfg.Auth.Tokens)
	}
	return found[0]
}

// TestRunConsoleSelfHealPrivilegedTokenNoManagedServiceNeverOpensBrowser is
// the P1 security regression this file inherited from console_url_test.go: a
// token whose scopes merely INCLUDE "observe" alongside a privileged scope
// (here "admin") must never be the one appended to the browser URL. Under
// self-heal, runConsole now writes a genuinely fresh observe-only token —
// but with no managed service installed in this test's scratch HOME to
// restart, it cannot confirm that token has taken effect. P1 fix: it no
// longer opens the browser at all in that case (a bare consoleURL would
// still 401 in bearer mode) -- it prints the token-added line, the
// no-managed-service note, and stops there, exit 0.
func TestRunConsoleSelfHealPrivilegedTokenNoManagedServiceNeverOpensBrowser(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "jill-net", listenAddrOf(t, server), "jill-observe-secret", []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin})

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 0 {
		t.Fatalf("expected no browser open at all (nothing confirmed to work yet), got %v", *opened)
	}
	if strings.Contains(output, "jill-observe-secret") {
		t.Fatalf("expected the printed output to never contain the privileged token, got %q", output)
	}
	if !strings.Contains(output, "console token added") {
		t.Fatalf("expected self-heal to report the new console token, got %q", output)
	}
	if !strings.Contains(output, "no managed service installed") {
		t.Fatalf("expected the no-managed-service restart guidance, got %q", output)
	}

	reloaded, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q) error = %v", configPath, err)
	}
	fresh := onlyObserveOnlyToken(t, reloaded)
	if strings.Contains(output, fresh.Value) {
		t.Fatalf("expected the newly written console token to never be printed, got %q", output)
	}
}

// TestRunConsoleSelfHealWriteOnlyTokenNoManagedServiceNeverOpensBrowser is
// the same shape as the privileged-token case above for a config whose only
// existing token carries "write", never "observe".
func TestRunConsoleSelfHealWriteOnlyTokenNoManagedServiceNeverOpensBrowser(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "mona-net", listenAddrOf(t, server), "mona-write-secret", []authn.Scope{authn.ScopeWrite})

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 0 {
		t.Fatalf("expected no browser open at all (nothing confirmed to work yet), got %v", *opened)
	}
	if strings.Contains(output, "mona-write-secret") {
		t.Fatalf("expected the printed output to never contain the write-scoped token, got %q", output)
	}
	if !strings.Contains(output, "console token added") {
		t.Fatalf("expected self-heal to report the new console token, got %q", output)
	}
}

// TestRunConsoleSelfHealWithManagedServiceOpensWithNewToken is the ready
// path end to end: a managed service is "installed" (a real unit file on
// disk so IsInstalled says yes), the fake Runner's Restart trivially
// succeeds, the health probe (still pointed at the same never-taken-down
// httptest server) confirms healthy immediately, and probeConsoleToken's
// live GET /v1/rooms also confirms the token itself works (consoleTestServer
// answers 200 to both) -- so runConsole must open the browser WITH the
// freshly minted token, and that token must be the exact one now on disk.
func TestRunConsoleSelfHealWithManagedServiceOpensWithNewToken(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())
	writeConsoleUnitFile(t, "nate-net")

	server := consoleTestServer(t, http.StatusOK, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "nate-net", listenAddrOf(t, server), "nate-write-secret", []authn.Scope{authn.ScopeWrite})

	wantBaseURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	reloaded, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q) error = %v", configPath, err)
	}
	fresh := onlyObserveOnlyToken(t, reloaded)
	if fresh.ID != "console" {
		t.Fatalf("expected the fresh token id to be %q, got %q", "console", fresh.ID)
	}

	wantOpenedURL := wantBaseURL + "?access_token=" + fresh.Value
	if len(*opened) != 1 || (*opened)[0] != wantOpenedURL {
		t.Fatalf("expected exactly one browser open of %q (with the freshly minted, now-effective token), got %v", wantOpenedURL, *opened)
	}
	if strings.Contains(output, fresh.Value) {
		t.Fatalf("expected the printed output to never contain the access token, got %q", output)
	}
	if !strings.Contains(output, "console token added") || !strings.Contains(output, "restarted the service") {
		t.Fatalf("expected both the token-added and restarted-service lines, got %q", output)
	}
}

// TestRunConsoleSelfHealPicksNonCollidingIDWhenConsoleIDInUse covers item
// 3's id-collision decision: when a token with id "console" already exists
// but does not carry exactly [observe] (so it is not the token
// consoleObserveToken looked for), self-heal must never touch or replace
// it — it picks a non-colliding id ("console-2") for the new observe-only
// token instead.
func TestRunConsoleSelfHealPicksNonCollidingIDWhenConsoleIDInUse(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())
	writeConsoleUnitFile(t, "opal-net")

	server := consoleTestServer(t, http.StatusOK, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "opal-net", listenAddrOf(t, server), "opal-write-secret", []authn.Scope{authn.ScopeWrite})

	// This config's only token already uses id "console" but carries
	// "write" -- a hand-configured token this flow must never overwrite or
	// widen the scope of.
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}
	collided := strings.Replace(string(original), "id: test-token", "id: console", 1)
	if collided == string(original) {
		t.Fatalf("fixture config did not contain the expected %q token id to rewrite", "test-token")
	}
	if err := os.WriteFile(configPath, []byte(collided), 0o600); err != nil {
		t.Fatalf("write collided fixture config: %v", err)
	}

	captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	reloaded, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q) error = %v", configPath, err)
	}
	fresh := onlyObserveOnlyToken(t, reloaded)
	if fresh.ID != "console-2" {
		t.Fatalf("expected the fresh observe-only token to use id %q (console already in use), got %q", "console-2", fresh.ID)
	}

	var originalStillPresent bool
	for _, token := range reloaded.Auth.Tokens {
		if token.ID == "console" && token.Value == "opal-write-secret" {
			originalStillPresent = true
			if len(token.Scopes) != 1 || token.Scopes[0] != authn.ScopeWrite {
				t.Fatalf("expected the original console-id token's scopes to stay [write], got %v", token.Scopes)
			}
		}
	}
	if !originalStillPresent {
		t.Fatalf("expected the original hand-configured console-id token to survive unchanged, got %+v", reloaded.Auth.Tokens)
	}
}

// TestRunConsoleSelfHealSkippedUnderPrintNeverMutatesConfig covers the
// scope decision behind self-heal only ever running on the actual
// browser-opening default path: --print (and, by the same code path,
// --no-open and a non-terminal stdout) is an explicit "just give me the
// URL" request, so it must stay side-effect-free — no token written, no
// service restarted — exactly like it was before self-heal existed.
func TestRunConsoleSelfHealSkippedUnderPrintNeverMutatesConfig(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "quinn-net", listenAddrOf(t, server), "quinn-write-secret", []authn.Scope{authn.ScopeWrite})

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config before: %v", err)
	}

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath, "--print"}, "test"); err != nil {
			t.Fatalf("console --print error = %v", err)
		}
	})

	if strings.TrimSpace(output) != wantURL {
		t.Fatalf("output = %q, want exactly %q under --print", output, wantURL)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open attempt under --print, got %v", *opened)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected --print to leave the config byte-for-byte unchanged (no self-heal write):\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
