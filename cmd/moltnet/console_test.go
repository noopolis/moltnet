package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/service"
)

// consoleBaseURL's own URL-derivation table lives in console_url_test.go,
// split out to keep this file under the repo's 400-line limit.

// --- test scaffolding ------------------------------------------------------

// consoleFakeServiceRunner never touches a real launchctl/systemctl; it
// backs every fake service.Manager this file constructs. It is named
// distinctly from service_test.go's fakeServiceRunner so both files can
// evolve independently.
type consoleFakeServiceRunner struct{}

func (consoleFakeServiceRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

// withConsoleFakeServiceManager swaps newServiceManager (service.go) for
// the duration of the test so runConsole's IsInstalled lookup never calls a
// real launchctl/systemctl, and restores it on cleanup.
func withConsoleFakeServiceManager(t *testing.T, goos string) {
	t.Helper()
	previous := newServiceManager
	newServiceManager = func() *service.Manager {
		return service.NewForOS(consoleFakeServiceRunner{}, goos)
	}
	t.Cleanup(func() { newServiceManager = previous })
}

// withFakeOpenURL swaps openURLFunc so no test ever spawns a real browser
// process, recording every URL it was asked to open.
func withFakeOpenURL(t *testing.T, err error) *[]string {
	t.Helper()
	var opened []string
	previous := openURLFunc
	openURLFunc = func(url string) error {
		opened = append(opened, url)
		return err
	}
	t.Cleanup(func() { openURLFunc = previous })
	return &opened
}

// withOutputTerminal swaps isOutputTerminal (style.go) for the duration of
// the test.
func withOutputTerminal(t *testing.T, tty bool) {
	t.Helper()
	previous := isOutputTerminal
	isOutputTerminal = func() bool { return tty }
	t.Cleanup(func() { isOutputTerminal = previous })
}

// writeConsoleTestConfig writes a valid Moltnet config for networkID whose
// server.listen_addr is listenAddr, reusing templates.go's validated
// defaultMoltnetConfig text so it stays in sync with the schema.
func writeConsoleTestConfig(t *testing.T, path, networkID, listenAddr string) {
	t.Helper()
	body := strings.Replace(defaultMoltnetConfig(networkID, networkID+" Moltnet"), `listen_addr: ":8787"`, `listen_addr: "`+listenAddr+`"`, 1)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// healthzServer starts an httptest server whose /healthz always answers
// with status. Its Listener address (host:port, no scheme) is what a test
// writes into server.listen_addr so runConsole dials it directly.
func healthzServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			http.NotFound(response, request)
			return
		}
		response.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

func listenAddrOf(t *testing.T, server *httptest.Server) string {
	t.Helper()
	addr := strings.TrimPrefix(server.URL, "http://")
	if addr == server.URL {
		t.Fatalf("unexpected httptest server URL %q", server.URL)
	}
	return addr
}

// --- health-check failure paths --------------------------------------------
//
// Split out to console_down_test.go: those four cases all assert on the
// two-stream captureStdoutAndStderr split (stdout empty, the down-server
// fact on stderr — the P2 fix), which kept pushing this file toward the
// repo's 400-line limit.

// --- success paths: --print / --no-open / TTY / non-TTY --------------------

func TestRunConsolePrintFlagPrintsURLOnlyNeverOpens(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "dana-net", listenAddrOf(t, server))

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath, "--print"}, "test"); err != nil {
			t.Fatalf("console --print error = %v", err)
		}
	})

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	if strings.TrimSpace(output) != wantURL {
		t.Fatalf("output = %q, want exactly %q", output, wantURL)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open attempt, got %v", *opened)
	}
}

func TestRunConsoleNoOpenKeepsReadyLineNeverOpens(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "erin-net", listenAddrOf(t, server))

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath, "--no-open"}, "test"); err != nil {
			t.Fatalf("console --no-open error = %v", err)
		}
	})

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	if !strings.Contains(output, "console") || !strings.Contains(output, wantURL) {
		t.Fatalf("expected a ready line naming the console URL, got %q", output)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open attempt, got %v", *opened)
	}
}

func TestRunConsoleNonTTYDefaultsToPrintOnly(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, false)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "frank-net", listenAddrOf(t, server))

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	if strings.TrimSpace(output) != wantURL {
		t.Fatalf("non-TTY output = %q, want exactly %q (print-only, no browser)", output, wantURL)
	}
	if len(*opened) != 0 {
		t.Fatalf("expected no browser open attempt from a non-TTY run, got %v", *opened)
	}
}

func TestRunConsoleTTYOpensBrowserAndPrintsReadyLine(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, nil)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "gina-net", listenAddrOf(t, server))

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 1 || (*opened)[0] != wantURL {
		t.Fatalf("expected exactly one browser open of %q, got %v", wantURL, *opened)
	}
	if !strings.Contains(output, "console") || !strings.Contains(output, wantURL) {
		t.Fatalf("expected a ready line naming the console URL, got %q", output)
	}
}

func TestRunConsoleOpenerFailureFallsBackToPrintingURL(t *testing.T) {
	withConsoleFakeServiceManager(t, "linux")
	opened := withFakeOpenURL(t, os.ErrNotExist)
	withOutputTerminal(t, true)
	t.Setenv("HOME", t.TempDir())

	server := healthzServer(t, http.StatusOK)
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConsoleTestConfig(t, configPath, "hank-net", listenAddrOf(t, server))

	wantURL := "http://" + listenAddrOf(t, server) + "/console/"
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "--config", configPath}, "test"); err != nil {
			t.Fatalf("console error = %v", err)
		}
	})

	if len(*opened) != 1 {
		t.Fatalf("expected exactly one (failed) browser open attempt, got %v", *opened)
	}
	if strings.TrimSpace(output) != wantURL {
		t.Fatalf("output = %q, want exactly %q after opener failure", output, wantURL)
	}
}

// --- misc dispatch -----------------------------------------------------

func TestRunConsoleHelp(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"console", "help"}, "test"); err != nil {
			t.Fatalf("console help error = %v", err)
		}
	})
	if !strings.Contains(output, "moltnet console") {
		t.Fatalf("unexpected help output %q", output)
	}
}

func TestRunConsoleRejectsPositionalArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := run(context.Background(), []string{"console", "extra-arg"}, "test")
	if err == nil {
		t.Fatal("expected an error for a positional argument")
	}
}

func TestRunConsoleRequiresConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	err := run(context.Background(), []string{"console"}, "test")
	if err == nil {
		t.Fatal("expected an error when no config can be resolved")
	}
}
