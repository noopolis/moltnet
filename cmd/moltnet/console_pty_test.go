package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/service"
)

// accessTokenPattern/redactAccessToken let this file's t.Fatalf calls name
// the failing URL or pty transcript without ever printing a live, working
// console token into test output or CI logs -- this file is the one place
// in the package that drives a real server and a real browser-open decision
// together, so its failure messages are the ones most likely to carry an
// actual working token.
var accessTokenPattern = regexp.MustCompile(`access_token=[^&\s]+`)

func redactAccessToken(s string) string {
	return accessTokenPattern.ReplaceAllString(s, "access_token=<redacted>")
}

// This file is the pty-capture half of the "prove it end to end" gate:
// console_e2e_test.go already proves the real HTTP status codes (401 ->
// self-heal -> 307 -> 200) against a real server, in-process, with
// isOutputTerminal faked via the usual test seam (withOutputTerminal). This
// file re-runs the exact fixed sequence -- `init --bearer` then `console`
// -- with the browser-open decision made by the real, unfaked isatty check
// against a genuine pseudo-terminal, following the same re-exec pattern
// relay_deploy_pty_test.go and prompt_hidden_pty_test.go already use in
// this package (see their doc comments): this test binary re-execs itself
// with an env var set, its stdin/stdout dup'd to a real pty's slave side.
// The service manager and browser opener are still faked -- this process
// never touches real launchd/systemd or a real browser -- but
// isOutputTerminal is not: that is the one dimension an in-process test
// cannot exercise.

const consolePTYHelperEnv = "MOLTNET_CONSOLE_PTY_HELPER"

// TestConsolePTYHelperProcess is never meaningfully run by `go test`
// itself -- startConsolePTYHelper below re-execs this same test binary
// with -test.run scoped to just this function and consolePTYHelperEnv set,
// so it runs as its own process with its real os.Stdin/os.Stdout dup'd to
// a pty's slave side. It runs `init --id pty-net --bearer` (mints both
// tokens for real) then `console --id pty-net` (opens with the console
// token init just minted) against a real server on a throwaway loopback
// port, and prints the URL it would have opened so the parent can fetch it
// for real and check the HTTP status.
func TestConsolePTYHelperProcess(t *testing.T) {
	if os.Getenv(consolePTYHelperEnv) == "" {
		return
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := runInit(context.Background(), []string{"--id", "pty-net", "--bearer"}); err != nil {
		fmt.Fprintf(os.Stdout, "HELPER-INIT-ERROR: %v\nHELPER-DONE\n", err)
		return
	}

	configPath := filepath.Join(home, ".moltnet", "pty-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	rewriteConfigListenAddr(t, configPath, addr)

	runner := &restartingServiceRunner{t: t, path: configPath}
	runner.current = startRealMoltnetServer(t, configPath)
	writeConsoleUnitFile(t, "pty-net")
	newServiceManager = func() *service.Manager { return service.NewForOS(runner, "linux") }

	var openedURL string
	openURLFunc = func(url string) error {
		openedURL = url
		return nil
	}

	if err := run(context.Background(), []string{"console", "--id", "pty-net"}, "test"); err != nil {
		fmt.Fprintf(os.Stdout, "HELPER-CONSOLE-ERROR: %v\nHELPER-DONE\n", err)
		return
	}

	fmt.Fprintf(os.Stdout, "HELPER-OPENED-URL: %s\nHELPER-DONE\n", openedURL)

	// Keep this process (and the real server it just started) alive so the
	// parent process can fetch openedURL for real before this helper
	// exits; the parent's t.Cleanup kills this process well before the
	// sleep would ever complete on its own.
	time.Sleep(10 * time.Second)
}

// startConsolePTYHelper re-execs this test binary as a fresh process
// running TestConsolePTYHelperProcess, with its stdin and stdout dup'd to
// slave (a real pty's slave side -- see requireTestPTY, openpty_test.go),
// mirroring startRelayDeployPTYHelper (relay_deploy_pty_test.go) one level
// up in this same file's scenario.
func startConsolePTYHelper(t *testing.T, slave *os.File) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestConsolePTYHelperProcess$")
	cmd.Env = append(os.Environ(), consolePTYHelperEnv+"=1")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd
}

// TestConsolePTYFreshInitBearerThenConsoleOverRealTerminal drives the
// fixed `init --bearer` -> `console` sequence over a genuine terminal and
// then, back in this (parent) process, fetches the exact URL the helper
// process's real isatty-gated browser-open path decided to open --
// proving both that the real terminal check took the browser-opening
// branch at all (not the --print/non-TTY fallback) and that the token it
// carries genuinely authenticates against the still-running real server.
func TestConsolePTYFreshInitBearerThenConsoleOverRealTerminal(t *testing.T) {
	master, slave := requireTestPTY(t)
	startConsolePTYHelper(t, slave)

	got := readMasterUntil(t, master, "HELPER-DONE", 10*time.Second)
	if strings.Contains(got, "HELPER-INIT-ERROR") || strings.Contains(got, "HELPER-CONSOLE-ERROR") {
		t.Fatalf("helper process reported an error over the real pty, transcript:\n%s", redactAccessToken(got))
	}

	const marker = "HELPER-OPENED-URL: "
	idx := strings.Index(got, marker)
	if idx < 0 {
		t.Fatalf("did not see the opened-URL marker over the real pty, transcript:\n%s", redactAccessToken(got))
	}
	line := got[idx+len(marker):]
	line = strings.SplitN(line, "\n", 2)[0]
	openedURL := strings.TrimSpace(line)
	if !strings.Contains(openedURL, "?access_token=") {
		t.Fatalf("expected the real-terminal run to open with the console token init --bearer minted, got %q\ntranscript:\n%s", redactAccessToken(openedURL), redactAccessToken(got))
	}
	if !strings.Contains(got, "console token added") && !strings.Contains(got, "console") {
		t.Fatalf("expected the styled ready line to have rendered over the real terminal, transcript:\n%s", redactAccessToken(got))
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New(): %v", err)
	}
	response, err := (&http.Client{Jar: jar}).Get(openedURL)
	if err != nil {
		t.Fatalf("GET %q: %v", redactAccessToken(openedURL), err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("final authenticated GET %q (opened over a real terminal) = %d, want 200", redactAccessToken(openedURL), response.StatusCode)
	}
}
