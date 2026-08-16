package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// These tests cover the interactive workers.dev subdomain claim prompt loop
// itself (attemptInteractiveWorkersDevSubdomainClaim,
// relay_deploy_subdomain_claim.go): retries, validation, exhaustion, and the
// Cloudflare error-code branches (10031, 10036) it distinguishes between.
// The full claim-then-redeploy end-to-end flows (a deploy actually hitting
// relaydeploy.ErrWorkersDevSubdomainUnclaimed and completing a second,
// successful Deploy call afterward) live in
// relay_deploy_subdomain_claim_redeploy_test.go instead — kept in a
// separate file so this one stays under the repo's 400-line split
// threshold.
//
// Every test here that drives a full run() sets CLOUDFLARE_API_TOKEN and
// passes --save-token, so the unrelated post-deploy token-save prompt never
// competes for a scripted promptReader answer with the subdomain claim
// prompt under test.

// TestRunRelayDeployInteractiveSubdomainClaimInvalidNameThenRetrySucceeds
// covers a locally-invalid name (caught by
// relaydeploy.ValidateWorkersDevSubdomainName before ever calling the
// Cloudflare API): it still counts as one of the two attempts, prints the
// specific validation reason, and the second (valid) attempt still
// succeeds.
func TestRunRelayDeployInteractiveSubdomainClaimInvalidNameThenRetrySucceeds(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "-not-valid-", "apresmoi")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, "must not start or end with a hyphen") {
		t.Fatalf("expected the local validation error to be printed, got %q", output)
	}
	// P2-2: a note: prefix, not a whole-line-yellow message with none.
	if !strings.Contains(output, yellow("note:")+` subdomain name "-not-valid-"`) {
		t.Fatalf("expected the validation error to carry a note: prefix, got %q", output)
	}
	if !strings.Contains(output, `claimed workers.dev subdomain "apresmoi"`) {
		t.Fatalf("expected the retry with a valid name to succeed, got %q", output)
	}
}

// TestRunRelayDeployInteractiveSubdomainClaimTakenNameRetries covers a name
// the fake Cloudflare server itself rejects (standing in for "already
// taken by another account" — Cloudflare documents no specific code for
// that): the first attempt is refused with the API's own message, the
// second (accepted) name succeeds.
func TestRunRelayDeployInteractiveSubdomainClaimTakenNameRetries(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "already-taken", "apresmoi")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, `could not claim "already-taken"`) {
		t.Fatalf("expected the rejected name to be named in the retry message, got %q", output)
	}
	// P2-2: a note: prefix, not a whole-line-yellow message with none.
	if !strings.Contains(output, yellow("note:")+` could not claim "already-taken"`) {
		t.Fatalf("expected the name-taken message to carry a note: prefix, got %q", output)
	}
	if !strings.Contains(output, `claimed workers.dev subdomain "apresmoi"`) {
		t.Fatalf("expected the second attempt to succeed, got %q", output)
	}
}

// TestRunRelayDeployInteractiveSubdomainClaimExhaustsAttemptsFallsBackToGuidance
// covers both attempts being rejected: run() still returns
// relaydeploy.ErrWorkersDevSubdomainUnclaimed (the original error, not a
// distinct claim-failure error), and the standard dashboard guidance is
// printed as the final fallback — no unbounded retry loop.
func TestRunRelayDeployInteractiveSubdomainClaimExhaustsAttemptsFallsBackToGuidance(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "nope-1", "nope-2")

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test")
	})
	if !errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("run() relay deploy error = %v, want it to still unwrap to ErrWorkersDevSubdomainUnclaimed", err)
	}
	if !strings.Contains(output, buildWorkersDevSubdomainGuidance("acme-net")) {
		t.Fatalf("expected the dashboard guidance once both claim attempts failed, got %q", output)
	}
	if strings.Contains(output, "relay live") {
		t.Fatalf("expected no successful deploy, got %q", output)
	}
}

// TestRunRelayDeployNonInteractiveWorkersDevSubdomainUnclaimedNeverPrompts
// covers the non-interactive path unchanged by this fix: no TTY means no
// claim prompt at all, just the (now claim-mentioning) dashboard guidance —
// matching how the pre-existing CLOUDFLARE_API_TOKEN guidance path behaves
// non-interactively.
func TestRunRelayDeployNonInteractiveWorkersDevSubdomainUnclaimedNeverPrompts(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	// Deliberately no withInteractiveOutput/withPromptAnswers: isInteractive()
	// and isOutputTerminal() both default to false in the test binary (no
	// real TTY on either stdin or captureStdout's os.Pipe stdout).

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test")
	})
	if !errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("run() relay deploy error = %v, want ErrWorkersDevSubdomainUnclaimed", err)
	}
	if want := "  Deploying relay for acme-net\n\n" + buildWorkersDevSubdomainGuidance("acme-net"); output != want {
		t.Fatalf("guidance output = %q, want the header followed by byte-identical buildWorkersDevSubdomainGuidance() = %q", output, want)
	}
	if strings.Contains(output, "choose a workers.dev subdomain") {
		t.Fatalf("expected no claim prompt on a non-interactive run, got %q", output)
	}
}

// TestBuildWorkersDevSubdomainGuidanceMentionsInteractiveClaim pins the
// updated guidance text: it now tells a non-interactive (or
// claim-exhausted) operator that running the command interactively claims
// the subdomain in place, instead of only pointing at the dashboard.
func TestBuildWorkersDevSubdomainGuidanceMentionsInteractiveClaim(t *testing.T) {
	guidance := buildWorkersDevSubdomainGuidance("acme-net")
	if !strings.Contains(guidance, "interactively") {
		t.Fatalf("expected the guidance to mention running interactively, got %q", guidance)
	}
	if !strings.Contains(guidance, "dash.cloudflare.com") {
		t.Fatalf("expected the guidance to keep the dashboard fallback, got %q", guidance)
	}
}

// TestMaxWorkersDevSubdomainClaimAttemptsIsTwo pins the exact retry budget:
// the whole point of the bound (see its doc comment — one retry, not an
// unbounded loop on typos) depends on this specific value. A mutation that
// silently widened it (e.g. to 50) would make
// TestRunRelayDeployInteractiveSubdomainClaimExhaustsAttemptsFallsBackToGuidance
// above hang or need many more scripted answers rather than failing
// cleanly, so pin the constant directly too.
func TestMaxWorkersDevSubdomainClaimAttemptsIsTwo(t *testing.T) {
	if maxWorkersDevSubdomainClaimAttempts != 2 {
		t.Fatalf("maxWorkersDevSubdomainClaimAttempts = %d, want 2", maxWorkersDevSubdomainClaimAttempts)
	}
}

// TestRunRelayDeployInteractiveSubdomainClaimEOFDeclinesToGuidance covers
// the P3 EOF/Ctrl-D fix: closing input at the claim prompt (no answer at
// all) must decline straight to the dashboard guidance, spending only the
// one attempt it actually made — not silently treat the closed input as an
// empty name and print "must not be empty" on both of the two attempts
// before falling back.
func TestRunRelayDeployInteractiveSubdomainClaimEOFDeclinesToGuidance(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t) // no lines at all: promptReader hits io.EOF immediately

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test")
	})
	if !errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("run() relay deploy error = %v, want it to still unwrap to ErrWorkersDevSubdomainUnclaimed", err)
	}
	if strings.Contains(output, "must not be empty") {
		t.Fatalf("expected no empty-name validation error on EOF/decline, got %q", output)
	}
	if got := strings.Count(output, workersDevSubdomainClaimPrompt); got != 1 {
		t.Fatalf("expected exactly one claim prompt attempt before declining on EOF, got %d in %q", got, output)
	}
	if !strings.Contains(output, buildWorkersDevSubdomainGuidance("acme-net")) {
		t.Fatalf("expected the dashboard guidance after declining, got %q", output)
	}
}

// TestAttemptInteractiveWorkersDevSubdomainClaimAccountAlreadyHasSubdomainContinues
// covers the P2 real-error-codes fix's 10036 branch directly (calling
// attemptInteractiveWorkersDevSubdomainClaim rather than driving it through
// a full run(), since the scenario — the account genuinely already has a
// claimed subdomain by the time the PUT runs — is a race Deploy's own
// earlier ErrWorkersDevSubdomainUnclaimed detection cannot itself model
// without contradicting its own fake's state): a PUT rejected with 10036
// must not be treated as a failure to retry — it must re-read the existing
// claim via GET and report it as claimed.
func TestAttemptInteractiveWorkersDevSubdomainClaimAccountAlreadyHasSubdomainContinues(t *testing.T) {
	fake := startFakeCloudflareCLIServerAccountAlreadyHasSubdomain(t, "", "already-claimed-name")
	client := relaydeploy.NewClientForTesting("test-token", fake.Server.URL, fake.Server.Client())
	withPromptAnswers(t, "apresmoi")

	var name string
	var ok bool
	output := captureStdout(t, func() {
		name, ok, _ = attemptInteractiveWorkersDevSubdomainClaim(context.Background(), client, "account-1", &sectionPrinter{})
	})
	if !ok {
		t.Fatalf("attemptInteractiveWorkersDevSubdomainClaim() ok = false, want true (10036 continues as already claimed), output = %q", output)
	}
	if name != "already-claimed-name" {
		t.Fatalf("attemptInteractiveWorkersDevSubdomainClaim() name = %q, want the account's existing subdomain %q", name, "already-claimed-name")
	}
	if !strings.Contains(output, `already has a workers.dev subdomain "already-claimed-name"`) {
		t.Fatalf("expected the existing subdomain to be named in the output, got %q", output)
	}
}

// TestAttemptInteractiveWorkersDevSubdomainClaimPropagationLagStopsRetrying
// covers the P3 copy fix: a PUT rejected with 10036 ("account already has a
// subdomain") whose follow-up GET recheck still reports the account
// unclaimed — Cloudflare's own read-after-write lag on the very claim it
// just told us about — must not retry (every further attempt would just hit
// the same 10036 again), and must report propagationPending=true so the
// caller knows not to print its own generic, and here self-contradictory,
// "has not claimed one yet" guidance on top of the specific reason this
// prints.
func TestAttemptInteractiveWorkersDevSubdomainClaimPropagationLagStopsRetrying(t *testing.T) {
	fake := startFakeCloudflareCLIServerAccountAlreadyHasSubdomainPropagationLag(t, "")
	client := relaydeploy.NewClientForTesting("test-token", fake.Server.URL, fake.Server.Client())
	withPromptAnswers(t, "apresmoi")

	var name string
	var ok, pending bool
	output := captureStdout(t, func() {
		name, ok, pending = attemptInteractiveWorkersDevSubdomainClaim(context.Background(), client, "account-1", &sectionPrinter{})
	})
	if ok {
		t.Fatalf("attemptInteractiveWorkersDevSubdomainClaim() ok = true, want false (propagation lag, not a success), output = %q", output)
	}
	if !pending {
		t.Fatalf("attemptInteractiveWorkersDevSubdomainClaim() propagationPending = false, want true, output = %q", output)
	}
	if name != "" {
		t.Fatalf("attemptInteractiveWorkersDevSubdomainClaim() name = %q, want empty", name)
	}
	if got := strings.Count(output, workersDevSubdomainClaimPrompt); got != 1 {
		t.Fatalf("expected exactly one claim prompt attempt before stopping on propagation lag (no futile retry), got %d in %q", got, output)
	}
	if !strings.Contains(output, "account already has a subdomain; Cloudflare can take a moment to report it — rerun in a minute") {
		t.Fatalf("expected the propagation-lag message, got %q", output)
	}
}
