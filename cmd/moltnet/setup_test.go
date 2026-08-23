package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
)

// captureSetupOutput is captureStdout's (main_test.go) counterpart for
// every test in this file: it swaps the package-global stdout the same way,
// but WITHOUT taking processStateMu itself. Setup's own tests dispatch
// through run() on this same goroutine, and run()'s child steps go through
// runChildInProcess (setup_runner_test.go), which *does* take
// processStateMu for its own nested stdout/stderr swap around the
// dispatched child command — a plain sync.Mutex is not reentrant, so this
// outer capture holding it too would deadlock the moment any test here
// actually executes a step (every test that does not use --print-commands
// or refuse before executing anything). This is safe because this package's
// tests never run in parallel (no t.Parallel() calls exist here); the inner
// adapter's own lock is what actually protects the swap against the one
// real hazard SETUP.md calls out (a concurrently PTY-backed stdout).
func captureSetupOutput(t *testing.T, fn func()) string {
	t.Helper()
	return captureStdoutLocked(t, fn)
}

// shrinkSetupHealthProbeTimeouts overrides the health-probe window to a few
// tens of milliseconds for the test's duration. Every test in this file
// that installs a service goes through withFakeServiceManager
// (service_test.go), whose fakeServiceRunner never actually spawns a
// listening process — a health probe against it is guaranteed to exhaust
// its window every time, so shrinking it keeps these tests fast without
// changing production behavior (setup_execute.go's own doc comment on why
// these are vars).
func shrinkSetupHealthProbeTimeouts(t *testing.T) {
	t.Helper()
	previousTimeout, previousInterval := setupHealthProbeTimeout, setupHealthProbeInterval
	setupHealthProbeTimeout, setupHealthProbeInterval = 100_000_000, 10_000_000 // 100ms / 10ms
	t.Cleanup(func() { setupHealthProbeTimeout, setupHealthProbeInterval = previousTimeout, previousInterval })
}

// writtenSetupConfigPath is the exact path a global-scope enter-through run
// (network id "local", the default) writes to under a HOME overridden to
// home.
func writtenSetupConfigPath(home string) string {
	return filepath.Join(home, ".moltnet", "local", "Moltnet")
}

// TestSetupEnterThroughGlobalCreatesNetwork is this unit's headline test:
// answering every question with Enter (an empty line, through the plain
// fallback prompt path) must produce a real, valid config — the
// "Enter-through-everything must be a valid complete path" rule applied to
// the whole wizard at once, not just one question at a time.
func TestSetupEnterThroughGlobalCreatesNetwork(t *testing.T) {
	withInProcessRunChild(t)
	withFakeServiceManager(t, "linux")
	withTestDefaultPort(t)
	shrinkSetupHealthProbeTimeouts(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// One empty line per question: Q1, Q2, Q4, Q5, Q6, Q7.
	withPromptAnswers(t, "", "", "", "", "", "")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})
	if !strings.Contains(output, "network initialized") {
		t.Fatalf("expected the init step to run, got %q", output)
	}
	if !strings.Contains(output, "service installed") {
		t.Fatalf("expected the service install step to run, got %q", output)
	}

	cfg, err := app.LoadFile(writtenSetupConfigPath(home), "")
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if cfg.NetworkID != app.DefaultNetworkID {
		t.Fatalf("cfg.NetworkID = %q, want %q", cfg.NetworkID, app.DefaultNetworkID)
	}
	if cfg.ListenAddr != fmt.Sprintf("127.0.0.1:%d", setupDefaultPort) {
		t.Fatalf("cfg.ListenAddr = %q, want the preflighted default port", cfg.ListenAddr)
	}
	if len(cfg.Rooms) != 1 || cfg.Rooms[0].ID != starterRoomID {
		t.Fatalf("cfg.Rooms = %+v, want just %q", cfg.Rooms, starterRoomID)
	}
}

// TestSetupEnterThroughFallsBackWhenDefaultPortHeld is the test the port
// preflight exists for: with the default port already bound, setup must
// still finish successfully, on a different, actually-usable port —
// exactly SETUP.md's "on a machine where 8787 is already held" case.
func TestSetupEnterThroughFallsBackWhenDefaultPortHeld(t *testing.T) {
	withInProcessRunChild(t)
	withFakeServiceManager(t, "linux")
	withTestDefaultPort(t)
	shrinkSetupHealthProbeTimeouts(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", setupDefaultPort))
	if err != nil {
		t.Skipf("could not hold the stand-in default port for this test: %v", err)
	}
	defer listener.Close()

	withPromptAnswers(t, "", "", "", "", "", "")
	captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})

	cfg, err := app.LoadFile(writtenSetupConfigPath(home), "")
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	held := fmt.Sprintf("127.0.0.1:%d", setupDefaultPort)
	if cfg.ListenAddr == held {
		t.Fatalf("expected setup to preflight around the held default port %q, got the same value back", held)
	}
	if !strings.HasPrefix(cfg.ListenAddr, "127.0.0.1:") {
		t.Fatalf("cfg.ListenAddr = %q, want it to stay on 127.0.0.1", cfg.ListenAddr)
	}
}

// TestSetupQ4WideBindWritesWildcardAndWarns exercises Q4's non-default
// branch: SETUP.md requires the warning be computed (app.
// NonLoopbackAnonymousWriteWarning via initNonLoopbackWarning, reused from
// Unit 1) at the moment of authoring, not a static string.
func TestSetupQ4WideBindWritesWildcardAndWarns(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Q1 default, Q2 default, Q4="2" (all network interfaces), Q5 default,
	// Q6="2" (no service — avoids needing a health probe against a wildcard
	// bind in this test), Q7 default.
	withPromptAnswers(t, "", "", "2", "", "2", "")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})
	if !strings.Contains(output, "warning:") || !strings.Contains(output, "all network interfaces") {
		t.Fatalf("expected the computed non-loopback warning, got %q", output)
	}

	cfg, err := app.LoadFile(writtenSetupConfigPath(home), "")
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if !strings.HasPrefix(cfg.ListenAddr, "0.0.0.0:") {
		t.Fatalf("cfg.ListenAddr = %q, want a 0.0.0.0 wildcard bind", cfg.ListenAddr)
	}
}

// TestSetupQ6DeclineServicePrintsForegroundCommand exercises Q6's "no"
// branch: SETUP.md requires "configured" (never "running") plus the exact
// foreground command, and no service-install step at all.
func TestSetupQ6DeclineServicePrintsForegroundCommand(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	withPromptAnswers(t, "", "", "", "", "2", "") // Q6="2" (no)

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})
	if strings.Contains(output, "service installed") {
		t.Fatalf("expected no service-install step when Q6 = no, got %q", output)
	}
	if !strings.Contains(output, "configured") {
		t.Fatalf("expected the configured (not running) outcome, got %q", output)
	}
	if !strings.Contains(output, "moltnet start --id local") {
		t.Fatalf("expected the exact foreground command, got %q", output)
	}
}

// TestSetupProjectScopeSkipsServiceQuestion exercises Q1 = "in this
// folder": Q6 must never be asked at all (SETUP.md: "skipped entirely when
// Q1 = in this folder"), and the closing foreground command is the bare
// `moltnet start` J3 describes (cwd discovery finds ./Moltnet with no
// flags).
func TestSetupProjectScopeSkipsServiceQuestion(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	t.Setenv("HOME", t.TempDir())

	// --project preselects Q1, consuming no read; remaining reads are Q2,
	// Q4, Q5, Q7 (Q6 skipped entirely).
	withPromptAnswers(t, "", "", "", "")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup", "--project"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})
	if strings.Contains(output, "Run as a service?") {
		t.Fatalf("expected Q6 to be skipped entirely for project scope, got %q", output)
	}
	if !strings.Contains(output, "moltnet start") || strings.Contains(output, "moltnet start --id") {
		t.Fatalf("expected the bare foreground command `moltnet start`, got %q", output)
	}

	if _, err := os.Stat(filepath.Join(projectDir, "Moltnet")); err != nil {
		t.Fatalf("expected a config written into the project directory: %v", err)
	}
}
