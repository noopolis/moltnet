package main

import (
	"context"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/service"
)

// This file is the "prove it end to end" gate: unlike every other console
// test in this package, it never fakes the server -- it runs a real
// internal/app.App (real auth policy, real console handler, real
// maybeSetConsoleAuthCookie cookie-setting) on a real loopback TCP
// listener, and drives runConsole against it exactly the way an operator
// would. The only things faked are the two things the hard rules forbid
// touching for real: the OS service manager (a fake Runner that, on
// restartingServiceRunner, actually tears down and rebuilds the real
// server from the freshly-written config -- proving self-heal's restart
// step genuinely reloads the token, not just that some function was
// called) and the browser opener (withFakeOpenURL, same as every other
// console test). HOME is a scratch temp dir; the server listens on an
// OS-assigned loopback port picked once via freeLoopbackAddr, never on
// :8787.

// freeLoopbackAddr asks the OS for a free loopback TCP port by briefly
// binding to 127.0.0.1:0 and releasing it, so the caller can put a fixed,
// known address into a Moltnet config before any real server exists yet.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeLoopbackAddr: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("freeLoopbackAddr close: %v", err)
	}
	return addr
}

// startRealMoltnetServer builds a real internal/app.App from the config at
// path and serves it on a real TCP listener bound to exactly
// cfg.ListenAddr (never httptest's own random 127.0.0.1:0 port), so
// runConsole's consoleBaseURL -- derived from the same config file --
// dials the exact address this server is actually listening on.
func startRealMoltnetServer(t *testing.T, path string) *httptest.Server {
	t.Helper()
	cfg, err := app.LoadConfigForPath(path, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q): %v", path, err)
	}
	instance, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New(): %v", err)
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		t.Fatalf("listen on %q: %v", cfg.ListenAddr, err)
	}
	server := httptest.NewUnstartedServer(instance.Handler())
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(func() {
		server.Close()
		_ = instance.Close()
	})
	return server
}

// restartingServiceRunner is the fake service.Runner behind this file's
// end-to-end proof: on the "restart" verb it actually closes the current
// real server and rebuilds a fresh one from path -- re-reading whatever
// self-heal just wrote to disk -- on the exact same address, so the
// restart genuinely reloads the new console token rather than merely
// recording that a restart was "requested". Every other verb (install's
// daemon-reload/enable, which this test never calls) is a harmless no-op,
// matching consoleFakeServiceRunner elsewhere in this package.
type restartingServiceRunner struct {
	t    *testing.T
	path string

	mu      sync.Mutex
	current *httptest.Server
}

func (r *restartingServiceRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	restart := false
	for _, arg := range args {
		if arg == "restart" {
			restart = true
		}
	}
	if !restart {
		return nil, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.Close()
	r.current = startRealMoltnetServer(r.t, r.path)
	return nil, nil
}

// TestRunConsoleEndToEndSelfHealAgainstRealServer proves the exact sequence
// the field problem was reported against, end to end, against a real
// server: a bearer-mode config that (like a config written before this fix,
// or bearer-enabled by hand) has only an operator token, no console token.
// Before this fix, `moltnet console` would open the browser on a raw 401.
// After: self-heal mints and writes a console token, "restarts" the
// managed service (which here really does reload the server process from
// the updated config), and only then opens the browser -- and the browser
// URL it opens genuinely authenticates against the real running server.
func TestRunConsoleEndToEndSelfHealAgainstRealServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "proof-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	// Simulates a config from before this fix: bearer mode, one privileged
	// operator token, no observe-only token at all.
	writeConsoleBearerTestConfig(t, configPath, "proof-net", addr, "proof-operator-secret",
		[]authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin, authn.ScopePair})

	runner := &restartingServiceRunner{t: t, path: configPath}
	runner.current = startRealMoltnetServer(t, configPath)
	writeConsoleUnitFile(t, "proof-net")

	previousManager := newServiceManager
	newServiceManager = func() *service.Manager { return service.NewForOS(runner, "linux") }
	t.Cleanup(func() { newServiceManager = previousManager })

	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)

	baseURL := "http://" + addr

	// Prove the bug this whole fix exists to close: before self-heal runs,
	// the real server really does 401 an unauthenticated /console/ GET.
	preFixResponse, err := http.Get(baseURL + "/console/")
	if err != nil {
		t.Fatalf("pre-fix GET /console/: %v", err)
	}
	preFixResponse.Body.Close()
	if preFixResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected the unauthenticated pre-fix request to 401, got %d", preFixResponse.StatusCode)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 1 {
		t.Fatalf("expected exactly one browser open, got %v", *opened)
	}
	openedURL := (*opened)[0]
	if !strings.Contains(openedURL, "?access_token=") {
		t.Fatalf("expected the opened URL to carry a fresh access_token (self-heal + restart should have succeeded), got %q\noutput:\n%s", openedURL, output)
	}
	if strings.Contains(openedURL, "proof-operator-secret") {
		t.Fatalf("expected the opened URL to never carry the privileged operator token, got %q", openedURL)
	}
	if !strings.Contains(output, "console token added") || !strings.Contains(output, "restarted the service") {
		t.Fatalf("expected self-heal + restart status lines, got %q", output)
	}

	// The real proof: fetch the exact URL the browser would have opened,
	// through a cookie jar (so the 307 that maybeSetConsoleAuthCookie
	// issues is followed with the cookie attached, precisely like a real
	// browser), against the now-restarted real server, and confirm 200 --
	// not the 401 an operator hitting this bug used to see.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New(): %v", err)
	}
	client := &http.Client{Jar: jar}
	consoleResponse, err := client.Get(openedURL)
	if err != nil {
		t.Fatalf("GET %q: %v", openedURL, err)
	}
	defer consoleResponse.Body.Close()
	if consoleResponse.StatusCode != http.StatusOK {
		t.Fatalf("final authenticated GET %q = %d, want 200 (this is exactly the request the field bug turned into a 401)", openedURL, consoleResponse.StatusCode)
	}

	// And a bare GET with no token, replayed against the very same
	// restarted server, must still 401 -- the fix hands the browser a
	// valid session, it does not open the console up.
	stillRequiresAuthResponse, err := http.Get(baseURL + "/console/")
	if err != nil {
		t.Fatalf("post-fix unauthenticated GET /console/: %v", err)
	}
	stillRequiresAuthResponse.Body.Close()
	if stillRequiresAuthResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected an unauthenticated request to still 401 after the fix, got %d", stillRequiresAuthResponse.StatusCode)
	}
}

// rewriteConfigListenAddr replaces the default "127.0.0.1:8787"
// server.listen_addr runInit writes with addr. Rebinding to a throwaway
// loopback port before this test ever starts a real listener is what keeps
// it from touching a real :8787 service the same machine might actually be
// running.
func rewriteConfigListenAddr(t *testing.T, path, addr string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	updated := strings.Replace(string(contents), `listen_addr: "127.0.0.1:8787"`, `listen_addr: "`+addr+`"`, 1)
	if updated == string(contents) {
		t.Fatalf("expected to find the default listen_addr to rewrite in:\n%s", contents)
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// TestRunConsoleEndToEndGoldenPathInitBearerNeedsNoSelfHeal proves the
// literal fixed sequence item 3 of the field report asked for: `init
// --id X --bearer` -> `service install --id X` -> `console --id X`. Since
// `init --bearer` now mints the console token itself, self-heal never even
// triggers here -- this is the straight-through happy path the console gap
// used to break, end to end against a real server.
func TestRunConsoleEndToEndGoldenPathInitBearerNeedsNoSelfHeal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "golden-net", "--bearer"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	configPath := filepath.Join(home, ".moltnet", "golden-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	rewriteConfigListenAddr(t, configPath, addr)

	// Stands in for `moltnet service install --id golden-net`: a real unit
	// file (so IsInstalled says yes) plus the real server actually running,
	// without ever touching real launchd/systemd.
	runner := &restartingServiceRunner{t: t, path: configPath}
	runner.current = startRealMoltnetServer(t, configPath)
	writeConsoleUnitFile(t, "golden-net")

	previousManager := newServiceManager
	newServiceManager = func() *service.Manager { return service.NewForOS(runner, "linux") }
	t.Cleanup(func() { newServiceManager = previousManager })

	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--id", "golden-net"}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 1 {
		t.Fatalf("expected exactly one browser open, got %v", *opened)
	}
	openedURL := (*opened)[0]
	if !strings.Contains(openedURL, "?access_token=") {
		t.Fatalf("expected the opened URL to already carry the console token init --bearer minted, got %q", openedURL)
	}
	if strings.Contains(output, "console token added") {
		t.Fatalf("expected no self-heal at all on the golden path (init --bearer already minted the console token), got %q", output)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New(): %v", err)
	}
	client := &http.Client{Jar: jar}
	consoleResponse, err := client.Get(openedURL)
	if err != nil {
		t.Fatalf("GET %q: %v", openedURL, err)
	}
	defer consoleResponse.Body.Close()
	if consoleResponse.StatusCode != http.StatusOK {
		t.Fatalf("final authenticated GET %q = %d, want 200 -- the console UI, not the field-reported 401", openedURL, consoleResponse.StatusCode)
	}
}
