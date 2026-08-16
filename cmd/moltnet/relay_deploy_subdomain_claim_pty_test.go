package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// This file covers the P1 fix (relay_deploy_subdomain_claim.go): a
// flushPendingTerminalInput() call before attemptInteractiveWorkersDevSubdomainClaim's
// attempt loop, matching the exact guard maybeSaveCloudflareToken already
// uses (relay_deploy_token.go:105) for the same class of bug — anything
// typed while the deploy that hit ErrWorkersDevSubdomainUnclaimed was still
// in flight must never silently answer (and burn an attempt of) the claim
// prompt that follows it. It shares prompt_hidden_pty_test.go's helper
// process, startPromptHiddenHelper, and drainMasterInBackground.

// claimFlushHelperCloudflareURLEnv names the env var
// runClaimFlushThenAttemptHelperScenario reads to find the fake Cloudflare
// server the parent test process started: a real net/http/httptest.Server
// bound to a loopback TCP port, reachable from the re-exec'd helper process
// (a separate OS process — see startPromptHiddenHelper's doc comment,
// prompt_hidden_pty_test.go) exactly as any other TCP client would reach
// it.
const claimFlushHelperCloudflareURLEnv = "MOLTNET_PROMPT_HIDDEN_HELPER_CLAIM_CLOUDFLARE_URL"

// claimFlushHelperClaimName is the subdomain name
// TestPromptHiddenPTYSubdomainClaimFlushDoesNotBurnAttempt writes to the
// pty after the stray typeahead, and the only name the fake Cloudflare
// server in that test accepts.
const claimFlushHelperClaimName = "apresmoi"

// runClaimFlushThenAttemptHelperScenario is the "claim-flush-then-attempt"
// case of TestPromptHiddenHelperProcess (prompt_hidden_pty_test.go): it
// sleeps to model the slow Deploy() call that, in production, already ran
// before attemptInteractiveWorkersDevSubdomainClaim is ever invoked (the
// function itself is only reached once relay deploy has already hit
// ErrWorkersDevSubdomainUnclaimed against the real API), then drives the
// real function against a fake Cloudflare server reachable at
// claimFlushHelperCloudflareURLEnv. If attemptInteractiveWorkersDevSubdomainClaim's
// own flushPendingTerminalInput call did not run before its first
// promptLine read, a stray typed-ahead "Enter" written to the pty during
// the sleep would be consumed as an empty answer to the claim prompt,
// burning one of only two attempts before the real name is ever sent.
func runClaimFlushThenAttemptHelperScenario() {
	baseURL := os.Getenv(claimFlushHelperCloudflareURLEnv)
	client := relaydeploy.NewClientForTesting("test-cloudflare-token", baseURL, http.DefaultClient)

	time.Sleep(300 * time.Millisecond)
	name, ok, _ := attemptInteractiveWorkersDevSubdomainClaim(context.Background(), client, "account-1", &sectionPrinter{})
	fmt.Fprintf(os.Stderr, "HELPER-RESULT:name=%q ok=%v\n", name, ok)
}

// TestPromptHiddenPTYSubdomainClaimFlushDoesNotBurnAttempt drives the real
// "claim-flush-then-attempt" helper scenario over a genuine pseudo-terminal
// and proves, against the kernel's own termios state (not just this
// package's own bookkeeping), that a stray "Enter" typed during the
// simulated pre-prompt delay never reaches promptLine's first read: if it
// did, the first attempt would be consumed by an empty name (printing
// "must not be empty" — visible on the pty, since this scenario never
// disables echo), leaving only the second (and last) attempt for the real
// answer instead of it succeeding on the first.
func TestPromptHiddenPTYSubdomainClaimFlushDoesNotBurnAttempt(t *testing.T) {
	master, slave := requireTestPTY(t)

	fake := startFakeCloudflareCLIServerUnclaimed(t, "test-cloudflare-token", claimFlushHelperClaimName)
	t.Setenv(claimFlushHelperCloudflareURLEnv, fake.Server.URL)

	cmd, stderr := startPromptHiddenHelper(t, "claim-flush-then-attempt", slave)

	// This scenario never disables echo, so every byte written to master
	// below gets echoed straight back into master's own read buffer by the
	// kernel; drain continuously in the background from the start, exactly
	// as TestPromptHiddenPTYMidDeployTypeaheadDoesNotAnswerSavePrompt does
	// for the same reason (prompt_hidden_pty_fixes_test.go) — the P0
	// input-flush fix's flush ioctl variant blocks until echoed output has
	// actually been read from the far end.
	output := drainMasterInBackground(master)

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	// Written immediately, well before the helper's 300ms simulated-deploy
	// sleep elapses and it ever calls flushPendingTerminalInput/reads —
	// exactly the "typed during the deploy" scenario this fix closes.
	if _, err := master.Write([]byte("\n")); err != nil {
		t.Fatalf("write stray typeahead to pty master: %v", err)
	}

	// Give the helper time to wake, flush, print its prompt, and start
	// blocking on the real read before sending the real answer.
	time.Sleep(450 * time.Millisecond)
	select {
	case err := <-waitErr:
		t.Fatalf("helper process exited before the real answer was sent (err=%v) — the stray typeahead answered the prompt; stderr: %s", err, stderr.String())
	default:
	}

	if _, err := master.Write([]byte(claimFlushHelperClaimName + "\n")); err != nil {
		t.Fatalf("write real answer to pty master: %v", err)
	}

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("helper process exited with error: %v (stderr: %s)", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper process did not exit within 5s after the real answer was written")
	}

	if got := stderr.String(); !strings.Contains(got, fmt.Sprintf("HELPER-RESULT:name=%q ok=true", claimFlushHelperClaimName)) {
		t.Fatalf("helper stderr = %q, want the claim to succeed with name %q", got, claimFlushHelperClaimName)
	}
	if got := output.String(); strings.Contains(got, "must not be empty") {
		t.Fatalf("expected no empty-name validation error (proof the stray Enter was discarded, not consumed as an attempt), got %q", got)
	}
}
