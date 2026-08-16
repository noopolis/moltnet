package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// This file's tests are a manual-verification gate for this fix round, not
// a permanent regression suite: they drive `relay deploy`'s real,
// unmodified fresh-flow and DNS-not-resolving paths (relay_deploy.go,
// relay_deploy_token.go) over a genuine pseudo-terminal — proving
// isOutputTerminal/isInteractive's real isatty checks actually turn on
// color and the interactive paste prompt, not just the isOutputTerminal
// seam override every other test in this package uses — following the same
// re-exec pattern prompt_hidden_pty_test.go uses for the same reason (see
// its own doc comment): this test binary re-execs itself with an env var
// set, its stdin/stdout dup'd to a real pty's slave side.

// relayDeployPTYHelperEnv selects which scenario a re-exec'd copy of this
// test binary runs, in TestRelayDeployPTYHelperProcess.
const relayDeployPTYHelperEnv = "MOLTNET_RELAY_DEPLOY_PTY_HELPER"

// relayDeployPTYHelperCloudflareURLEnv and relayDeployPTYHelperConfigEnv
// pass the parent's fake Cloudflare server URL and Moltnet config path
// through to the re-exec'd helper process, which is a real, separate OS
// process (see startRelayDeployPTYHelper) and so cannot share the parent's
// in-memory newRelayDeployClient override any other way.
const (
	relayDeployPTYHelperCloudflareURLEnv = "MOLTNET_RELAY_DEPLOY_PTY_HELPER_CLOUDFLARE_URL"
	relayDeployPTYHelperConfigEnv        = "MOLTNET_RELAY_DEPLOY_PTY_HELPER_CONFIG"
)

// TestRelayDeployPTYHelperProcess is never meaningfully run by `go test`
// itself — the driving tests below re-exec this same test binary with
// -test.run scoped to just this function and relayDeployPTYHelperEnv set,
// so it runs as its own separate process with its real os.Stdin/os.Stdout
// dup'd to a pty's slave side. Deliberately never overrides
// isOutputTerminal/isInteractive: the whole point is exercising their real,
// non-test-seam isatty checks against a genuine terminal.
func TestRelayDeployPTYHelperProcess(t *testing.T) {
	scenario := os.Getenv(relayDeployPTYHelperEnv)
	if scenario == "" {
		return
	}

	fakeURL := os.Getenv(relayDeployPTYHelperCloudflareURLEnv)
	newRelayDeployClient = func(token string) *relaydeploy.Client {
		return relaydeploy.NewClientForTesting(token, fakeURL, http.DefaultClient)
	}
	configPath := os.Getenv(relayDeployPTYHelperConfigEnv)

	switch scenario {
	case "dns-note":
		// A freshly deployed hostname that never resolves (P2-5): the whole
		// point of this scenario is proving the real terminal path prints
		// the restored hostname-naming note, not a color-invariant seam.
		resolveRelayDeployHostname = func(ctx context.Context, hostname string) bool { return false }
		_ = run(context.Background(), []string{"relay", "deploy", "--id", "acme-net", "--config", configPath}, "test")
	case "fresh-flow":
		// No CLOUDFLARE_API_TOKEN, nothing stored: the real interactive
		// paste-prompt guidance path (maybePromptForCloudflareToken,
		// relay_deploy_token.go), including the P3 restored blank line
		// ahead of "paste token (input hidden): ".
		resolveRelayDeployHostname = func(ctx context.Context, hostname string) bool { return true }
		_ = run(context.Background(), []string{"relay", "deploy", "--id", "acme-net", "--config", configPath}, "test")
	}
}

// startRelayDeployPTYHelper re-execs this test binary as a fresh process
// running TestRelayDeployPTYHelperProcess under scenario, with its stdin
// and stdout dup'd to slave (a real pty's slave side — see requireTestPTY,
// openpty_test.go), mirroring startPromptHiddenHelper
// (prompt_hidden_pty_test.go) exactly, one level down in this same package.
func startRelayDeployPTYHelper(t *testing.T, scenario, cloudflareURL, configPath string, slave *os.File) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRelayDeployPTYHelperProcess$")
	cmd.Env = append(os.Environ(),
		relayDeployPTYHelperEnv+"="+scenario,
		relayDeployPTYHelperCloudflareURLEnv+"="+cloudflareURL,
		relayDeployPTYHelperConfigEnv+"="+configPath,
	)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process (scenario %q): %v", scenario, err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd
}

// TestRelayDeployPTYFreshFlowShowsGuidanceAndPromptsOverRealTerminal is the
// fresh-flow half of this fix round's pty-capture gate: over a genuine
// terminal, with no CLOUDFLARE_API_TOKEN and nothing stored, `relay deploy`
// must print the token-creation guidance, a blank line (P3 restored gap),
// then the paste prompt — read from the real master side of the pty, not
// this package's usual swapped-io.Reader test seam.
func TestRelayDeployPTYFreshFlowShowsGuidanceAndPromptsOverRealTerminal(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServer(t, "pasted-cf-token")

	master, slave := requireTestPTY(t)
	startRelayDeployPTYHelper(t, "fresh-flow", fake.Server.URL, path, slave)

	got := readMasterUntil(t, master, "paste token (input hidden): ", 5*time.Second)
	if !strings.Contains(got, "No Cloudflare API token found") {
		t.Fatalf("expected the token-creation guidance over the real pty, got %q", got)
	}
	// PTYs translate a bare "\n" to "\r\n"; the blank line the P3 fix
	// restored between the dim hint and the prompt shows up as "\r\n\r\n"
	// here for that reason. Real color is on over this real terminal, so
	// the dim hint carries its own trailing reset escape (\x1b[0m) right
	// before that "\r\n\r\n" — checked for loosely (Contains, not an exact
	// span) since the assertion itself runs un-colored in the parent test
	// process.
	if !strings.Contains(got, "Create Token → copy)") || !strings.Contains(got, "\r\n\r\n  paste token (input hidden): ") {
		t.Fatalf("expected a real blank line between the guidance and the paste prompt (P3 restored-blank-line fix), got %q", got)
	}
}

// TestRelayDeployPTYDNSNoteNamesHostnameOverRealTerminal is the DNS-note
// half of this fix round's pty-capture gate (P2-5): over a genuine
// terminal, a deploy whose freshly claimed hostname is not resolving yet
// must print the note naming that hostname specifically.
func TestRelayDeployPTYDNSNoteNamesHostnameOverRealTerminal(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	os.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	t.Cleanup(func() { os.Unsetenv("CLOUDFLARE_API_TOKEN") })

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")

	master, slave := requireTestPTY(t)
	startRelayDeployPTYHelper(t, "dns-note", fake.Server.URL, path, slave)

	got := readMasterUntil(t, master, "is not resolving yet", 5*time.Second)
	// Real color is on over this real terminal, so "note:" carries its own
	// ANSI escapes here (unlike this package's usual non-TTY test output) —
	// checked loosely rather than against yellow("note:"), since that
	// helper would render plain in this assertion's own (non-pty) process.
	if !strings.Contains(got, "note:") || !strings.Contains(got, "moltnet-relay.acme.workers.dev is not resolving yet") {
		t.Fatalf("expected the note to name the deployed hostname over the real pty, got %q", got)
	}
}
