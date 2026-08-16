package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// These tests drive `moltnet relay deploy` end to end through a deploy that
// hits relaydeploy.ErrWorkersDevSubdomainUnclaimed on a real (fake) TTY: the
// interactive claim prompt, and — on a successful claim —
// relaydeploy.Deploy re-run in full (relay_deploy.go). Split out from
// relay_deploy_subdomain_claim_test.go, which covers the claim prompt
// loop's own retry/validation/error-code mechanics in isolation, to keep
// both files under the repo's 400-line split threshold.
//
// Every test here sets CLOUDFLARE_API_TOKEN (except the three-prompt test,
// which deliberately leaves it unset to also exercise the paste prompt) and
// passes --save-token, so the unrelated post-deploy token-save prompt never
// competes for a scripted promptReader answer with the subdomain claim
// prompt under test.

// TestRunRelayDeployInteractiveSubdomainClaimThenRedeploySucceeds drives the
// field-observed failure mode this fix maps to the sentinel at every step
// (not just EnableWorkersDevRoute): startFakeCloudflareCLIServerUnclaimed
// fails upload/secret/route with Cloudflare error code 10063 until a PUT
// /workers/subdomain claims one, so this is the belt-and-braces
// upload-10063 detection path. See
// TestRunRelayDeployInteractiveSubdomainClaimViaSubdomainCheckThenRedeploySucceeds
// below for the primary WorkersDevSubdomain-GET-check detection path.
func TestRunRelayDeployInteractiveSubdomainClaimThenRedeploySucceeds(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "apresmoi")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, workersDevSubdomainClaimPrompt) {
		t.Fatalf("expected the claim prompt %q, got %q", workersDevSubdomainClaimPrompt, output)
	}
	if !strings.Contains(output, buildWorkersDevSubdomainClaimIntro()) {
		t.Fatalf("expected the claim intro before the prompt, got %q", output)
	}
	if !strings.Contains(output, `claimed workers.dev subdomain "apresmoi"`) {
		t.Fatalf("expected a claim confirmation, got %q", output)
	}
	if !strings.Contains(output, `deployed relay Worker "moltnet-relay"`) {
		t.Fatalf("expected the redeploy after claiming to succeed, got %q", output)
	}
	if strings.Contains(output, buildWorkersDevSubdomainGuidance("acme-net")) {
		t.Fatalf("expected no fallback dashboard guidance once the interactive claim succeeded, got %q", output)
	}
}

// TestRunRelayDeployInteractiveSubdomainClaimViaSubdomainCheckThenRedeploySucceeds
// is TestRunRelayDeployInteractiveSubdomainClaimThenRedeploySucceeds's
// counterpart for the P1 fix (deploy.go: the !claimed branch's Result must
// still carry AccountID): it drives the *primary*
// ErrWorkersDevSubdomainUnclaimed detection path — relaydeploy.Deploy's own
// WorkersDevSubdomain GET check, reached only because upload and secret
// both succeed on this fake even though the account is unclaimed
// (startFakeCloudflareCLIServerUnclaimedViaSubdomainCheck) — rather than
// the belt-and-braces upload-10063 path every other test here exercises.
// Before the fix, that branch returned a zeroed Result, so the interactive
// claim PUT that follows would target /accounts//workers/subdomain (an
// empty account id) instead of the real account, and this whole flow would
// never reach a successful redeploy.
func TestRunRelayDeployInteractiveSubdomainClaimViaSubdomainCheckThenRedeploySucceeds(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimedViaSubdomainCheck(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "apresmoi")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, workersDevSubdomainClaimPrompt) {
		t.Fatalf("expected the claim prompt %q, got %q", workersDevSubdomainClaimPrompt, output)
	}
	if !strings.Contains(output, `claimed workers.dev subdomain "apresmoi"`) {
		t.Fatalf("expected a claim confirmation (proof the claim PUT reached the real account, not an empty account id), got %q", output)
	}
	if !strings.Contains(output, `deployed relay Worker "moltnet-relay"`) {
		t.Fatalf("expected the redeploy after claiming to succeed, got %q", output)
	}
	if strings.Contains(output, buildWorkersDevSubdomainGuidance("acme-net")) {
		t.Fatalf("expected no fallback dashboard guidance once the interactive claim succeeded, got %q", output)
	}
}

// TestRunRelayDeployInteractiveSubdomainClaimRedeploySucceedsButStillReportsUnclaimedPrintsLagGuidance
// covers the P2 post-claim propagation-lag messaging fix: the claim PUT
// itself succeeds (reported as claimed to the operator), but the redeploy
// Deploy re-run immediately afterward still finds the account unclaimed —
// Cloudflare's own read-after-write lag (startFakeCloudflareCLIServerClaimLag).
// The output must carry the specific propagation-lag guidance, never the
// generic "has not claimed" guidance, which would be actively misleading
// once the claim has already succeeded.
func TestRunRelayDeployInteractiveSubdomainClaimRedeploySucceedsButStillReportsUnclaimedPrintsLagGuidance(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerClaimLag(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "apresmoi")

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test")
	})
	if !errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("run() relay deploy error = %v, want it to still unwrap to ErrWorkersDevSubdomainUnclaimed", err)
	}
	if !strings.Contains(output, `claimed workers.dev subdomain "apresmoi"`) {
		t.Fatalf("expected the claim PUT itself to still be reported as succeeded, got %q", output)
	}
	if !strings.Contains(output, buildWorkersDevSubdomainClaimLagGuidance("apresmoi", "acme-net")) {
		t.Fatalf("expected the propagation-lag guidance, got %q", output)
	}
	if strings.Contains(output, buildWorkersDevSubdomainGuidance("acme-net")) {
		t.Fatalf("expected no generic 'has not claimed' fallback guidance once the claim itself succeeded, got %q", output)
	}
}

// TestRunRelayDeploySubdomainClaimPropagationLagPrintsOnlySpecificGuidance
// covers the P3 copy fix end to end: the 10036-rejected claim whose GET
// recheck still reports the account unclaimed must leave the operator with
// only the propagation-specific message — never the generic dashboard
// guidance, which would flatly contradict it (that guidance says the
// account has not claimed a subdomain at all, when Cloudflare just told us,
// via 10036, that it has).
func TestRunRelayDeploySubdomainClaimPropagationLagPrintsOnlySpecificGuidance(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerAccountAlreadyHasSubdomainPropagationLag(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "apresmoi")

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test")
	})
	if !errors.Is(err, relaydeploy.ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("run() relay deploy error = %v, want it to still unwrap to ErrWorkersDevSubdomainUnclaimed", err)
	}
	if !strings.Contains(output, "account already has a subdomain; Cloudflare can take a moment to report it — rerun in a minute") {
		t.Fatalf("expected the propagation-lag message, got %q", output)
	}
	if strings.Contains(output, buildWorkersDevSubdomainGuidance("acme-net")) {
		t.Fatalf("expected no contradictory generic 'has not claimed one yet' guidance, got %q", output)
	}
	if strings.Contains(output, `deployed relay Worker`) {
		t.Fatalf("expected no successful deploy, got %q", output)
	}
}

// TestRunRelayDeployInteractiveSubdomainClaimRedeployReusesPriorToken covers
// the highest-consequence property of the whole claim-then-redeploy flow:
// Deploy is documented and re-run in full once a claim succeeds (see
// attemptInteractiveWorkersDevSubdomainClaim's doc comment), and that
// second Deploy call must still reuse an existing RELAY_TOKEN
// (Options.PriorToken) exactly as the first, aborted attempt would have —
// rotating it on a redeploy the operator did not ask to rotate would break
// every pairing already using this relay at once.
func TestRunRelayDeployInteractiveSubdomainClaimRedeployReusesPriorToken(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	credentialsPath := relaydeploy.CredentialsPath(path)
	const priorToken = "existing-relay-token-should-survive-claim-redeploy"
	if err := relaydeploy.SaveCredentials(credentialsPath, relaydeploy.RelayCredentials{
		URL:        "wss://moltnet-relay.some-old-subdomain.workers.dev",
		Token:      priorToken,
		ScriptName: relaydeploy.DefaultScriptName,
	}); err != nil {
		t.Fatalf("seed prior relay credentials: %v", err)
	}

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "apresmoi")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, `deployed relay Worker "moltnet-relay"`) {
		t.Fatalf("expected the claim-then-redeploy to succeed, got %q", output)
	}

	creds, ok, err := relaydeploy.LoadCredentials(credentialsPath)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadCredentials() ok = false, want saved credentials after the redeploy")
	}
	if creds.Token != priorToken {
		t.Fatalf("redeploy RELAY_TOKEN = %q, want the prior token %q preserved (rotating it would break every existing pairing on this relay)", creds.Token, priorToken)
	}
}

// TestRunRelayDeployThreePromptEndToEnd drives the likeliest real first-run
// path, previously untested end to end: no CLOUDFLARE_API_TOKEN and no
// stored token (paste prompt), a Cloudflare account with no claimed
// workers.dev subdomain (claim prompt), then the post-deploy pasted-token
// save offer — all three interactive prompts answered in the one run,
// sharing the same promptReader seam in order.
func TestRunRelayDeployThreePromptEndToEnd(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	const pastedToken = "pasted-cf-token-xyz"
	fake := startFakeCloudflareCLIServerUnclaimed(t, pastedToken, "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, pastedToken, "apresmoi", "y")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})

	tokenPath := relaydeploy.CloudflareTokenPath(path)
	saveOfferPrompt := fmt.Sprintf("save to %s", tokenPath)

	if !strings.Contains(output, "paste token (input hidden)") {
		t.Fatalf("expected the token paste prompt, got %q", output)
	}
	if !strings.Contains(output, workersDevSubdomainClaimPrompt) {
		t.Fatalf("expected the subdomain claim prompt, got %q", output)
	}
	if !strings.Contains(output, `claimed workers.dev subdomain "apresmoi"`) {
		t.Fatalf("expected the claim to succeed, got %q", output)
	}
	if !strings.Contains(output, `deployed relay Worker "moltnet-relay"`) {
		t.Fatalf("expected the redeploy after claiming to succeed, got %q", output)
	}
	if !strings.Contains(output, saveOfferPrompt) {
		t.Fatalf("expected the post-deploy pasted-token save offer naming %s, got %q", tokenPath, output)
	}

	// The three prompts must appear in this exact order — the paste prompt
	// (no CLOUDFLARE_API_TOKEN yet), then the subdomain claim prompt (only
	// reachable once a token exists to deploy with), then the post-deploy
	// save offer (only reachable once the deploy itself has succeeded) — not
	// merely all be present somewhere in the output. Indexes, not
	// substring-count, so a regression that reorders (rather than drops or
	// duplicates) a prompt still fails this.
	pasteIndex := strings.Index(output, "paste token (input hidden)")
	claimIndex := strings.Index(output, workersDevSubdomainClaimPrompt)
	saveOfferIndex := strings.Index(output, saveOfferPrompt)
	if !(pasteIndex < claimIndex && claimIndex < saveOfferIndex) {
		t.Fatalf("prompts out of order: paste@%d, claim@%d, save-offer@%d, want strictly ascending; output = %q", pasteIndex, claimIndex, saveOfferIndex, output)
	}

	if got := cloudflareTokenFileContents(t, tokenPath); got != pastedToken {
		t.Fatalf("saved Cloudflare token = %q, want the pasted token %q", got, pastedToken)
	}
}
