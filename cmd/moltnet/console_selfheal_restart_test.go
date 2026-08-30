package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/service"
)

// This file covers the two restart-outcome fixes on `moltnet console`'s
// self-heal path (console_selfheal_test.go, split out purely to keep that
// file under the repo's 400-line limit): item 2, a restart that never comes
// back must fail loudly instead of reporting success, and item 4, the
// --no-restart opt-out that skips the automatic restart attempt entirely.

// restartTornDownRunner is a fake service.Runner whose "restart" verb flips
// tornDown to true and never flips it back -- simulating a restart that
// tears the server down and never brings it back up. Every other verb
// (IsInstalled's own file-based check never calls Run at all) is a
// harmless no-op, matching consoleFakeServiceRunner elsewhere in this
// package.
type restartTornDownRunner struct {
	tornDown *atomic.Bool
}

func (r restartTornDownRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	for _, arg := range args {
		if arg == "restart" {
			r.tornDown.Store(true)
		}
	}
	return nil, nil
}

// TestRunConsoleSelfHealRestartNeverComesBackFailsLoud covers P2: a restart
// self-heal itself attempts, whose /healthz never answers again within
// consoleRestartHealthTimeout, must be a genuine command failure -- no green
// ready line, no browser open, nonzero exit (errConsoleRestartFailed) --
// never the old behavior of a bounded wait, a warning, and then a
// misleading "✓ console <url>" over a server that is still down.
func TestRunConsoleSelfHealRestartNeverComesBackFailsLoud(t *testing.T) {
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())
	writeConsoleUnitFile(t, "priya-net")

	// /healthz answers 200 until the fake runner's "restart" verb flips
	// tornDown, then it answers 503 forever after -- a restart that tore the
	// server down and never brought it back, the exact P2 field scenario.
	var tornDown atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/healthz" && !tornDown.Load():
			response.WriteHeader(http.StatusOK)
		case request.URL.Path == "/healthz":
			response.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)

	previousManager := newServiceManager
	newServiceManager = func() *service.Manager {
		return service.NewForOS(restartTornDownRunner{tornDown: &tornDown}, "linux")
	}
	t.Cleanup(func() { newServiceManager = previousManager })

	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "priya-net", listenAddrOf(t, server), "priya-write-secret", []authn.Scope{authn.ScopeWrite})

	var runErr error
	output := captureStdout(t, func() {
		runErr = run(context.Background(), []string{"console", "--config", configPath}, "test")
	})

	if !errors.Is(runErr, errConsoleRestartFailed) {
		t.Fatalf("console error = %v, want errConsoleRestartFailed", runErr)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open when the restart never came back, got %v", *opened)
	}
	if strings.Contains(output, "✓ console  ") {
		t.Fatalf("expected no green console-ready line over a restart that never came back, got %q", output)
	}
	if !strings.Contains(output, "has not answered /healthz yet") {
		t.Fatalf("expected the restart-did-not-come-back warning, got %q", output)
	}
}

// TestRunConsoleNoRestartWritesTokenButNeverRestarts covers item 4:
// --no-restart lets self-heal write a fresh console token but never
// restarts the service itself, printing the exact manual restart command
// instead. Nothing is confirmed live, so no browser is opened either.
func TestRunConsoleNoRestartWritesTokenButNeverRestarts(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())
	writeConsoleUnitFile(t, "quill-net")

	server := consoleTestServer(t, http.StatusOK, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleBearerTestConfig(t, configPath, "quill-net", listenAddrOf(t, server), "quill-write-secret", []authn.Scope{authn.ScopeWrite})

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath, "--no-restart"}, "test"); err != nil {
			t.Fatalf("console --no-restart error = %v", err)
		}
	})

	if len(*opened) != 0 {
		t.Fatalf("expected no browser open under --no-restart (token never confirmed live), got %v", *opened)
	}
	if !strings.Contains(output, "console token added") {
		t.Fatalf("expected self-heal to still write the token under --no-restart, got %q", output)
	}
	if !strings.Contains(output, "--no-restart") {
		t.Fatalf("expected the --no-restart note naming the manual restart command, got %q", output)
	}
	if strings.Contains(output, "restarted the service") {
		t.Fatalf("expected --no-restart to never actually restart the service, got %q", output)
	}

	reloaded, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q) error = %v", configPath, err)
	}
	onlyObserveOnlyToken(t, reloaded)
}
