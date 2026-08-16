package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// These tests cover the interactive Cloudflare-token paste prompt
// (maybePromptForCloudflareToken, promptHidden — relay_deploy.go,
// relay_deploy_token.go, prompt_hidden.go): the flow that replaces
// `export CLOUDFLARE_API_TOKEN=...` with an inline paste when relay deploy
// finds no token and both stdin and stdout are a TTY. The real echo-off
// behavior (stty -echo/-echo against a controlling terminal) is only
// exercisable with a real pty, not go test's own stdin — see the
// disableTerminalEcho noop-path tests below, and the separate manual pty
// run recorded for this change.

// withInteractiveOutput forces isOutputTerminal to true for the test's
// duration, restoring it afterward — withPromptAnswers (uninstall_test.go)
// already forces isInteractive (stdin) to true, but the interactive-prompt
// gate requires both stdin AND stdout to be a TTY (maybePromptForCloudflareToken),
// so tests exercising that gate need this too.
func withInteractiveOutput(t *testing.T) {
	t.Helper()
	previous := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	t.Cleanup(func() { isOutputTerminal = previous })
}

// TestRunRelayDeployInteractivePromptPasteDeploySaveYes exercises the full
// happy path end to end: no CLOUDFLARE_API_TOKEN, no stored token, a real
// (fake) TTY on both stdin and stdout — relay deploy prints the guidance
// essentials, prompts for a paste, deploys using the pasted token against
// the fake Cloudflare server, then (default yes) saves it.
func TestRunRelayDeployInteractivePromptPasteDeploySaveYes(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	fake := startFakeCloudflareCLIServer(t, "pasted-cf-token")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "pasted-cf-token", "y")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})

	if !strings.Contains(output, "paste token (input hidden): ") {
		t.Fatalf("expected the paste prompt, got %q", output)
	}
	if !strings.Contains(output, "dash.cloudflare.com/profile/api-tokens") {
		t.Fatalf("expected the token-creation deep link before the prompt, got %q", output)
	}
	if !strings.Contains(output, `deployed relay Worker "moltnet-relay"`) {
		t.Fatalf("expected a successful deploy using the pasted token, got %q", output)
	}
	if !strings.Contains(output, "save to "+tokenPath+" (0600)? [Y/n] ") {
		t.Fatalf("expected the default-yes save prompt naming %q, got %q", tokenPath, output)
	}
	if strings.Contains(output, "pasted-cf-token") {
		t.Fatalf("expected the pasted token value never to be printed, got %q", output)
	}
	if got := cloudflareTokenFileContents(t, tokenPath); got != "pasted-cf-token" {
		t.Fatalf("saved token = %q, want pasted-cf-token", got)
	}
	// The non-interactive missing-token guidance's export instructions must
	// not leak into the interactive prompt's shorter intro.
	if strings.Contains(output, "export CLOUDFLARE_API_TOKEN") {
		t.Fatalf("expected no export-and-retry guidance on the interactive prompt path, got %q", output)
	}
}

// TestRunRelayDeployInteractivePromptSaveDeclineSkipsSave covers a bare "n"
// (or anything but empty/"y") to the post-deploy save prompt: the deploy
// still succeeds, but nothing is written to tokenPath, and there is no nag.
func TestRunRelayDeployInteractivePromptSaveDeclineSkipsSave(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	fake := startFakeCloudflareCLIServer(t, "pasted-cf-token")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "pasted-cf-token", "n")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, `deployed relay Worker "moltnet-relay"`) {
		t.Fatalf("expected a successful deploy even when the save offer is declined, got %q", output)
	}
	if _, ok, err := relaydeploy.LoadCloudflareToken(tokenPath); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() after declining = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	if strings.Contains(output, "saved Cloudflare API token") {
		t.Fatalf("expected no save confirmation after declining, got %q", output)
	}
}

// TestRunRelayDeployInteractivePromptBareEnterSaves covers the P2-3 fix end
// to end: a bare Enter (empty answer, not an explicit "y") at the post-deploy
// save prompt still saves the pasted token, pinning the prompt's default-yes
// behavior through the full run() path, not just promptYesNoDefaultYes in
// isolation (see TestPromptYesNoDefaultYesBareEnterSaves).
func TestRunRelayDeployInteractivePromptBareEnterSaves(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	fake := startFakeCloudflareCLIServer(t, "pasted-cf-token")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "pasted-cf-token", "")

	captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if got := cloudflareTokenFileContents(t, tokenPath); got != "pasted-cf-token" {
		t.Fatalf("saved token = %q, want pasted-cf-token to have been saved on a bare Enter", got)
	}
}

// TestRunRelayDeployInteractivePromptPastedTokenSaveTokenFlag covers the
// P3-11 fix: --save-token on the pasted-token path saves unconditionally,
// without asking the post-deploy save prompt at all — mirroring the
// existing env-token --save-token coverage
// (TestRunRelayDeployEndToEndSaveTokenFlag, relay_deploy_token_test.go) for
// the newer paste path.
func TestRunRelayDeployInteractivePromptPastedTokenSaveTokenFlag(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	fake := startFakeCloudflareCLIServer(t, "pasted-cf-token")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	// Only one scripted answer: the paste itself. If --save-token still
	// prompted for confirmation, this would either hang or fail with an
	// unexpected read — proof the flag skips the prompt outright.
	withPromptAnswers(t, "pasted-cf-token")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy --save-token error = %v", err)
		}
	})
	if strings.Contains(output, "(0600)? [Y/n]") {
		t.Fatalf("expected --save-token to skip the save prompt entirely, got %q", output)
	}
	if got := cloudflareTokenFileContents(t, tokenPath); got != "pasted-cf-token" {
		t.Fatalf("saved token = %q, want pasted-cf-token", got)
	}
	if !strings.Contains(output, "saved Cloudflare API token to "+tokenPath) {
		t.Fatalf("expected a save confirmation naming %q, got %q", tokenPath, output)
	}
}

// TestRunRelayDeployInteractivePromptEmptyInputErrors covers a bare Enter
// (empty paste) at the token prompt (P3-9): the operator already saw the
// guidance right before the prompt, so this is a short "no token pasted"
// error rather than a reprint of the full export-and-retry guidance block —
// and it must never attempt a Cloudflare call (no fake server wired up
// here — one would fail the test outright if dialed).
func TestRunRelayDeployInteractivePromptEmptyInputErrors(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	withInteractiveOutput(t)
	withPromptAnswers(t, "")

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test")
		if err == nil || err.Error() != "no token pasted" {
			t.Fatalf("run() relay deploy error = %v, want the short no-token-pasted error", err)
		}
	})
	if !strings.Contains(output, "paste token (input hidden): ") {
		t.Fatalf("expected the paste prompt to have been shown, got %q", output)
	}
	if strings.Contains(output, "export CLOUDFLARE_API_TOKEN") {
		t.Fatalf("expected no reprint of the full missing-token guidance after an empty paste, got %q", output)
	}
}

// TestRunRelayDeployInteractivePromptRejectedTokenNotSaved covers a pasted
// token the fake Cloudflare server rejects (401, e.g. a typo'd paste): a
// clear error propagates, nothing is saved, and — since runRelayDeploy
// returns before ever reaching the post-deploy save prompt — the run never
// re-prompts.
func TestRunRelayDeployInteractivePromptRejectedTokenNotSaved(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	// wantToken names the only token the fake server accepts; the pasted
	// token below deliberately does not match it, so every call 401s.
	fake := startFakeCloudflareCLIServer(t, "the-real-token")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "wrong-pasted-token")

	var err error
	captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path}, "test")
	})
	if err == nil {
		t.Fatal("run() relay deploy error = nil, want the rejected-token error to propagate")
	}
	if !strings.Contains(err.Error(), "verify Cloudflare API token") {
		t.Fatalf("run() relay deploy error = %v, want a clear verify-token failure", err)
	}
	if strings.Contains(err.Error(), "wrong-pasted-token") {
		t.Fatalf("run() relay deploy error = %v, want the rejected token value never printed", err)
	}
	if _, ok, loadErr := relaydeploy.LoadCloudflareToken(tokenPath); loadErr != nil || ok {
		t.Fatalf("LoadCloudflareToken() after a rejected pasted token = (ok=%v, err=%v), want (false, nil)", ok, loadErr)
	}
}

// TestRunRelayDeployPromptRequiresBothStdinAndStdoutTTY proves the
// interactive-prompt gate is an AND, not just isInteractive(): with stdin
// forced interactive but stdout NOT a terminal, relay deploy must still
// fall back to the non-interactive guidance-and-error path, never a
// prompt.
func TestRunRelayDeployPromptRequiresBothStdinAndStdoutTTY(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	// withPromptAnswers forces isInteractive() (stdin) true; isOutputTerminal
	// is deliberately left at its default (false, since captureStdout's
	// os.Pipe writer is never a TTY).
	withPromptAnswers(t, "should-never-be-read")

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test")
		if err == nil || !strings.Contains(err.Error(), "CLOUDFLARE_API_TOKEN") {
			t.Fatalf("run() relay deploy error = %v, want the missing-token error", err)
		}
	})
	if strings.Contains(output, "paste token") {
		t.Fatalf("expected no paste prompt when stdout is not a TTY, got %q", output)
	}
	if output != buildMissingCloudflareTokenGuidance("acme-net") {
		t.Fatalf("guidance output = %q, want byte-identical to buildMissingCloudflareTokenGuidance()", output)
	}
}

// TestPromptHiddenReadsAndTrimsFromFakeReader covers promptHidden's read
// and trim behavior directly, using the same promptReader test seam as
// promptYesNo — it is what the paste prompt reduces to once
// disableTerminalEcho has already no-op'd (see the tests below for that).
func TestPromptHiddenReadsAndTrimsFromFakeReader(t *testing.T) {
	withPromptAnswers(t, "  padded-token-value  ")

	var got string
	var err error
	output := captureStdout(t, func() {
		got, err = promptHidden("paste token (input hidden): ")
	})
	if err != nil {
		t.Fatalf("promptHidden() error = %v", err)
	}
	if got != "padded-token-value" {
		t.Fatalf("promptHidden() = %q, want surrounding whitespace trimmed", got)
	}
	if !strings.Contains(output, "paste token (input hidden): ") {
		t.Fatalf("expected the question to be printed, got %q", output)
	}
	if strings.Contains(output, "padded-token-value") {
		t.Fatalf("expected the input itself never to be echoed, got %q", output)
	}
}

// TestPromptHiddenEmptyLineReturnsEmptyString covers a bare Enter: an empty
// line trims to "", nil — the caller (maybePromptForCloudflareToken) treats
// this identically to EOF (Ctrl-D), matching promptYesNo's own EOF
// handling.
func TestPromptHiddenEmptyLineReturnsEmptyString(t *testing.T) {
	withPromptAnswers(t, "")

	var got string
	var err error
	captureStdout(t, func() {
		got, err = promptHidden("paste token (input hidden): ")
	})
	if err != nil {
		t.Fatalf("promptHidden() error = %v", err)
	}
	if got != "" {
		t.Fatalf("promptHidden() = %q, want empty string for a bare Enter", got)
	}
}

// TestDisableTerminalEchoNoopWhenPromptReaderNotStdin covers the primary
// safety property of disableTerminalEcho without a real TTY: whenever
// promptReader has been swapped away from the real os.Stdin (every test in
// this package, via withPromptAnswers), it must never attempt to run
// `stty` at all — only return a harmless, idempotent no-op restore. This is
// the branch every automated test in this suite actually exercises.
func TestDisableTerminalEchoNoopWhenPromptReaderNotStdin(t *testing.T) {
	withPromptAnswers(t, "irrelevant")

	restore, ok := disableTerminalEcho()
	if restore == nil {
		t.Fatal("disableTerminalEcho() returned a nil restore func")
	}
	if !ok {
		t.Fatal("disableTerminalEcho() ok = false, want true for a legitimate no-op (not a failure)")
	}
	// Idempotency: calling restore more than once (the normal call plus a
	// hypothetical SIGINT-handler call racing it) must never panic.
	restore()
	restore()
}

// TestDisableTerminalEchoNoopWhenNotInteractive covers the other no-op
// gate: even if promptReader were the real os.Stdin, a non-interactive
// stdin (isInteractive() false — the default in this test binary, and the
// real-world piped/CI case) must not attempt `stty` either.
func TestDisableTerminalEchoNoopWhenNotInteractive(t *testing.T) {
	previousReader := promptReader
	promptReader = os.Stdin
	t.Cleanup(func() { promptReader = previousReader })

	previousInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = previousInteractive })

	restore, ok := disableTerminalEcho()
	if restore == nil {
		t.Fatal("disableTerminalEcho() returned a nil restore func")
	}
	if !ok {
		t.Fatal("disableTerminalEcho() ok = false, want true for a legitimate no-op (not a failure)")
	}
	restore()
}
