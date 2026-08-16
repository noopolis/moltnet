package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/service"
)

// The health-check failure paths for `moltnet console` live in this file,
// split out of console_test.go (which was approaching the repo's 400-line
// limit) once the P2 fix moved this failure prose from stdout to stderr —
// every case here now needs the two-stream captureStdoutAndStderr helper
// (main_test.go) instead of plain captureStdout, so splitting them out
// together keeps console_test.go's own success-path cases the simpler,
// single-stream shape. Shared scaffolding (withConsoleFakeServiceManager,
// withFakeOpenURL, withOutputTerminal, writeConsoleTestConfig,
// healthzServer, listenAddrOf) stays defined in console_test.go and is used
// from here too, same package.

func TestRunConsoleServerErrorStatusNeverOpensBrowser(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusInternalServerError)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "alice-net", listenAddrOf(t, server))

	stdoutOutput, stderrOutput := captureStdoutAndStderr(t, func() {
		err := run(context.Background(), []string{"console", "--config", configPath}, "test")
		if err == nil {
			t.Fatal("expected an error when /healthz does not return 200")
		}
		if !strings.Contains(err.Error(), "alice-net") {
			t.Fatalf("error %v does not name the network", err)
		}
	})

	// P2: the down-server fact and its suggested next command print to
	// stderr, never stdout — stdout must stay empty so a script capturing
	// it (`URL=$(moltnet console --print)`) is never poisoned with prose.
	if stdoutOutput != "" {
		t.Fatalf("expected empty stdout on a down server, got %q", stdoutOutput)
	}
	if !strings.Contains(stderrOutput, "server not running for alice-net") {
		t.Fatalf("unexpected stderr %q", stderrOutput)
	}
	// configPath was passed explicitly via --config, so the suggested next
	// command must echo it back verbatim rather than --id alice-net: a bare
	// --id only resolves a config under ~/.moltnet, which would not find
	// this one.
	if !strings.Contains(stderrOutput, "moltnet service install --config "+configPath) {
		t.Fatalf("expected the install suggestion (no unit installed) echoing --config in %q", stderrOutput)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open attempt, got %v", *opened)
	}
}

func TestRunConsoleServerDownWithIDSuggestsInstallByID(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	home := t.TempDir()
	t.Setenv("HOME", home)

	server := healthzServer(t, http.StatusInternalServerError)
	configPath := filepath.Join(home, ".moltnet", "iris-net", "Moltnet")
	writeConsoleTestConfig(t, configPath, "iris-net", listenAddrOf(t, server))

	stdoutOutput, stderrOutput := captureStdoutAndStderr(t, func() {
		err := run(context.Background(), []string{"console", "--id", "iris-net"}, "test")
		if err == nil {
			t.Fatal("expected an error when /healthz does not return 200")
		}
	})

	if stdoutOutput != "" {
		t.Fatalf("expected empty stdout on a down server, got %q", stdoutOutput)
	}
	// No --config was given here, only --id, so the suggested command must
	// echo --id back (a --config path was never part of the invocation to
	// echo in the first place).
	if !strings.Contains(stderrOutput, "moltnet service install --id iris-net") {
		t.Fatalf("expected the install suggestion to echo --id iris-net in %q", stderrOutput)
	}
	if strings.Contains(stderrOutput, "--config") {
		t.Fatalf("did not expect --config in the suggestion when only --id was given: %q", stderrOutput)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open attempt, got %v", *opened)
	}
}

func TestRunConsoleConnectionRefusedNeverOpensBrowser(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	// Start and immediately close a server to get a real "connection
	// refused" on a definite, momentarily-real loopback address.
	server := healthzServer(t, http.StatusOK)
	addr := listenAddrOf(t, server)
	server.Close()

	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "bob-net", addr)

	stdoutOutput, stderrOutput := captureStdoutAndStderr(t, func() {
		err := run(context.Background(), []string{"console", "--config", configPath}, "test")
		if err == nil {
			t.Fatal("expected an error when the server is unreachable")
		}
	})

	if stdoutOutput != "" {
		t.Fatalf("expected empty stdout on a down server, got %q", stdoutOutput)
	}
	if !strings.Contains(stderrOutput, "server not running for bob-net") {
		t.Fatalf("unexpected stderr %q", stderrOutput)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open attempt, got %v", *opened)
	}
}

// TestConsoleUsageAgreesWithServerDownSuggestion pins buildConsoleUsage's
// prose to the exact command reportConsoleServerDown actually prints for the
// service-installed-but-not-answering case ("moltnet service start --id
// <id>", never the foreground "moltnet start --id <id>", which would race
// the already-loaded unit). The two lived out of sync once already: the help
// text named the racing foreground command while reportConsoleServerDown
// printed the correct one.
func TestConsoleUsageAgreesWithServerDownSuggestion(t *testing.T) {
	usage := buildConsoleUsage()
	const wantSuggestion = `"moltnet service start --id <id>"`
	if !strings.Contains(usage, wantSuggestion) {
		t.Fatalf("buildConsoleUsage() missing %s; got:\n%s", wantSuggestion, usage)
	}
	// Guard against the exact regression: the bare foreground command must
	// not appear as a quoted suggestion anywhere in the help text.
	const staleSuggestion = `"moltnet start --id <id>"`
	if strings.Contains(usage, staleSuggestion) {
		t.Fatalf("buildConsoleUsage() still contains the stale foreground suggestion %s; got:\n%s", staleSuggestion, usage)
	}

	withConsoleFakeServiceManager(t, "linux")
	withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	unitPath, err := service.SystemdUnitPath("dana-net")
	if err != nil {
		t.Fatalf("SystemdUnitPath: %v", err)
	}
	if mkErr := os.MkdirAll(filepath.Dir(unitPath), 0o700); mkErr != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(unitPath), mkErr)
	}
	if writeErr := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o600); writeErr != nil {
		t.Fatalf("write %q: %v", unitPath, writeErr)
	}

	server := healthzServer(t, http.StatusServiceUnavailable)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "dana-net", listenAddrOf(t, server))

	_, stderrOutput := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err == nil {
			t.Fatal("expected an error when /healthz does not return 200")
		}
	})

	// The actual printed command must use the same "moltnet service start"
	// verb the help text documents -- never the bare "moltnet start" the
	// help text used to (incorrectly) name.
	if !strings.Contains(stderrOutput, "moltnet service start --config "+configPath) {
		t.Fatalf("reportConsoleServerDown printed %q, expected it to use \"moltnet service start\" to match buildConsoleUsage's documented command", stderrOutput)
	}
}

func TestRunConsoleServerDownSuggestsStartWhenServiceInstalled(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	unitPath, err := service.SystemdUnitPath("carol-net")
	if err != nil {
		t.Fatalf("SystemdUnitPath: %v", err)
	}
	if mkErr := os.MkdirAll(filepath.Dir(unitPath), 0o700); mkErr != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(unitPath), mkErr)
	}
	if writeErr := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o600); writeErr != nil {
		t.Fatalf("write %q: %v", unitPath, writeErr)
	}

	server := healthzServer(t, http.StatusServiceUnavailable)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "carol-net", listenAddrOf(t, server))

	stdoutOutput, stderrOutput := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err == nil {
			t.Fatal("expected an error when /healthz does not return 200")
		}
	})

	if stdoutOutput != "" {
		t.Fatalf("expected empty stdout on a down server, got %q", stdoutOutput)
	}
	// configPath was passed explicitly via --config here too, so the
	// suggestion echoes --config, not --id; it also must be the foreground-
	// racing "moltnet start" no longer, but "moltnet service start", which
	// controls the already-loaded unit instead of racing it.
	if !strings.Contains(stderrOutput, "moltnet service start --config "+configPath) {
		t.Fatalf("expected the service-start suggestion (unit already installed) echoing --config in %q", stderrOutput)
	}
	if strings.Contains(stderrOutput, "service install") {
		t.Fatalf("did not expect the install suggestion once a unit exists: %q", stderrOutput)
	}
}
