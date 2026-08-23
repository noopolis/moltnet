package main

import (
	"context"
	"strings"
	"testing"
)

// These tests cover the `relay deploy --subdomain` flag itself (Unit 0 of
// the setup-wizard prerequisites, .plans/SETUP.md "Driving the terminal"
// precondition 2): supplying a workers.dev subdomain name non-interactively
// instead of the interactive prompt in relay_deploy_subdomain_claim.go,
// which has no non-interactive path at all. Every test here deliberately
// leaves stdin/stdout non-interactive (the test binary's default — no
// withInteractiveOutput/withPromptAnswers) so the flag is proven to bypass
// the prompt entirely, not merely to work alongside it.

// TestRunRelayDeploySubdomainFlagSuppliesNameNonInteractively covers the
// "no claim, --subdomain given" branch end to end on a non-TTY run: the
// name is claimed without ever prompting, and the deploy that depends on it
// (upload, secret, route) still succeeds.
func TestRunRelayDeploySubdomainFlagSuppliesNameNonInteractively(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--subdomain", "apresmoi"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if strings.Contains(output, workersDevSubdomainClaimPrompt) {
		t.Fatalf("expected no interactive claim prompt when --subdomain is given, got %q", output)
	}
	if !strings.Contains(output, "relay live") {
		t.Fatalf("expected the deploy to succeed using the flag-supplied name, got %q", output)
	}
	if got := fake.claimAttemptCount(); got != 1 {
		t.Fatalf("claimAttemptCount() = %d, want exactly 1 (the non-interactive claim)", got)
	}
	// This is a first-ever, permanent, account-level claim -- it must be
	// acknowledged on stdout (result.Claimed, relaydeploy/deploy.go),
	// otherwise indistinguishable from adopting an account that already had
	// one (see the negative assertion in the adopt-existing-claim test).
	if !strings.Contains(output, `claimed workers.dev subdomain "apresmoi"`) {
		t.Fatalf("expected an acknowledgement of the fresh permanent claim, got %q", output)
	}
}

// TestRunRelayDeploySubdomainFlagNormalizesUppercaseInput covers the CLI
// layer's own normalize-before-validate step (relay_deploy.go): an
// uppercase --subdomain value must still claim and match the lowercase name
// the account ends up with, exactly as the interactive prompt's
// NormalizeWorkersDevSubdomainName call does for a typed name.
func TestRunRelayDeploySubdomainFlagNormalizesUppercaseInput(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--subdomain", "APresMoi"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, "moltnet-relay.apresmoi.workers.dev") {
		t.Fatalf("expected the deploy to use the normalized (lowercase) subdomain, got %q", output)
	}
}

// TestRunRelayDeploySubdomainFlagInvalidNameIsUsageErrorBeforeHeader covers
// the P3 no-header-on-immediate-error pattern this flag follows (same as
// --token-env, relay_deploy_test.go): an invalid --subdomain name is a
// config-independent usage error caught before the "Deploying relay for
// <id>" header ever prints, and before any Cloudflare API call.
func TestRunRelayDeploySubdomainFlagInvalidNameIsUsageErrorBeforeHeader(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path, "--subdomain", "-not-valid-"}, "test")
	})
	if err == nil || !strings.Contains(err.Error(), "--subdomain") {
		t.Fatalf("run() relay deploy error = %v, want a --subdomain usage error", err)
	}
	if output != "" {
		t.Fatalf("expected no output (no header, no Cloudflare call) before this immediate usage error, got %q", output)
	}
}

// TestRunRelayDeploySubdomainFlagBlankAfterTrimIsUsageError pins the fix for
// a real gap: a whitespace-only --subdomain "   " used to trim down to "",
// silently treated exactly as if the flag had never been passed at all --
// the run then died on the generic unclaimed-subdomain error with no hint
// that --subdomain itself had been ignored. It must now be a --subdomain
// usage error instead, same as an invalid name, and before any Cloudflare
// call (no fake Cloudflare server is even wired up for this test).
func TestRunRelayDeploySubdomainFlagBlankAfterTrimIsUsageError(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path, "--subdomain", "   "}, "test")
	})
	if err == nil || !strings.Contains(err.Error(), "--subdomain") {
		t.Fatalf("run() relay deploy error = %v, want a --subdomain usage error", err)
	}
	if output != "" {
		t.Fatalf("expected no output before this immediate usage error, got %q", output)
	}
}

// TestRunRelayDeploySubdomainFlagAdoptsMatchingExistingClaimWithoutReclaiming
// covers "existing account claim → adopt it, proceed": passing --subdomain
// naming the account's own already-claimed subdomain must not attempt a
// redundant claim PUT (Cloudflare's claim is a one-time, permanent
// operation).
func TestRunRelayDeploySubdomainFlagAdoptsMatchingExistingClaimWithoutReclaiming(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServer(t, "env-cf-token") // already claimed "acme"
	withRelayDeployFakeCloudflare(t, fake)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--subdomain", "acme"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, "relay live") {
		t.Fatalf("expected the deploy to succeed adopting the existing claim, got %q", output)
	}
	if got := fake.claimAttemptCount(); got != 0 {
		t.Fatalf("claimAttemptCount() = %d, want 0 (a name matching the existing claim must not re-claim)", got)
	}
	// Adopting an existing claim must not print the fresh-claim
	// acknowledgement -- nothing new or permanent was actually committed by
	// this run, unlike TestRunRelayDeploySubdomainFlagSuppliesNameNonInteractively's
	// genuinely fresh claim.
	if strings.Contains(output, "claimed workers.dev subdomain") {
		t.Fatalf("expected no fresh-claim acknowledgement when adopting an existing claim, got %q", output)
	}
}

// TestRunRelayDeploySubdomainFlagConflictRefusesWithClearMessage covers "a
// requested name that conflicts with a different existing claim → refuse":
// the deploy must fail, name both the requested and the account's actual
// subdomain in the error, and never attempt the claim PUT (Cloudflare would
// only reject it, and the flag's whole point is to avoid a doomed call).
func TestRunRelayDeploySubdomainFlagConflictRefusesWithClearMessage(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServer(t, "env-cf-token") // already claimed "acme"
	withRelayDeployFakeCloudflare(t, fake)

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path, "--subdomain", "someone-else"}, "test")
	})
	if err == nil || !strings.Contains(err.Error(), "someone-else") || !strings.Contains(err.Error(), "acme") {
		t.Fatalf("run() relay deploy error = %v, want it to name both the requested and existing subdomains", err)
	}
	if strings.Contains(output, "relay live") {
		t.Fatalf("expected no successful deploy on a subdomain conflict, got %q", output)
	}
	if got := fake.claimAttemptCount(); got != 0 {
		t.Fatalf("claimAttemptCount() = %d, want 0 (a conflicting name must never reach the claim PUT)", got)
	}
}

// TestRunRelayDeployNonInteractiveNoSubdomainFlagStillFailsCleanly is the
// non-flag counterpart proving nothing regressed for the plain non-TTY path
// once --subdomain exists: with no claim and no flag, the printed guidance
// now names --subdomain as the fix (rather than only the dashboard steps),
// before any upload — mirroring
// TestRunRelayDeployNonInteractiveWorkersDevSubdomainUnclaimedNeverPrompts's
// no-prompt assertion, but pinned here as the "what to pass instead" half of
// this flag's own test coverage.
func TestRunRelayDeployNonInteractiveNoSubdomainFlagStillFailsCleanly(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "env-cf-token", "apresmoi")
	withRelayDeployFakeCloudflare(t, fake)

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path}, "test")
	})
	if err == nil {
		t.Fatal("run() relay deploy error = nil, want ErrWorkersDevSubdomainUnclaimed")
	}
	if strings.Contains(output, "relay live") {
		t.Fatalf("expected no successful deploy, got %q", output)
	}
	if !strings.Contains(output, "--subdomain") {
		t.Fatalf("expected the guidance to name --subdomain as the non-interactive fix, got %q", output)
	}
	if got := fake.claimAttemptCount(); got != 0 {
		t.Fatalf("claimAttemptCount() = %d, want 0 (no --subdomain means no claim attempt at all)", got)
	}
}
