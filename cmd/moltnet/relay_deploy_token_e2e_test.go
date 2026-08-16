package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// These tests cover the per-network stored Cloudflare API token end to
// end: --forget-token, the corrupt-stored-file fallback paths, --save-token,
// the TTY save-offer prompt, and stored-token deploy precedence — all
// driven through run() against a fakeCloudflareCLIServer
// (relay_deploy_cloudflare_test.go). Pure resolution/save-decision logic
// tested directly against resolveCloudflareAPIToken, maybeSaveCloudflareToken,
// and promptYesNoDefaultYes lives in relay_deploy_token_test.go, split out
// to keep both files under the repo's 400-line-per-file rule.

func TestRunRelayDeployForgetTokenRemovesStoredFile(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	if err := relaydeploy.SaveCloudflareToken(tokenPath, "cf-token"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--forget-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy --forget-token error = %v", err)
		}
	})
	if !strings.Contains(output, "removed stored Cloudflare API token "+tokenPath) {
		t.Fatalf("unexpected --forget-token output %q", output)
	}
	if _, ok, err := relaydeploy.LoadCloudflareToken(tokenPath); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() after --forget-token = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestRunRelayDeployForgetTokenWhenNothingStoredSaysSo(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--forget-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy --forget-token error = %v", err)
		}
	})
	if !strings.Contains(output, "no stored Cloudflare API token at "+tokenPath) {
		t.Fatalf("unexpected --forget-token output %q", output)
	}
}

func TestRunRelayDeployForgetTokenDoesNotRequireCloudflareToken(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--forget-token"}, "test"); err != nil {
		t.Fatalf("run() relay deploy --forget-token error = %v, want --forget-token to work without CLOUDFLARE_API_TOKEN", err)
	}
}

// TestRunRelayDeployGuidanceUnchangedWhenNothingStored pins that the
// missing-token guidance (deep link included) is identical whether or not a
// .moltnet/cloudflare.json has ever existed for this network — there is
// nothing stored in either case, so the guidance path is exactly the
// pre-existing one.
func TestRunRelayDeployGuidanceUnchangedWhenNothingStored(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test")
		if err == nil || !strings.Contains(err.Error(), "CLOUDFLARE_API_TOKEN") {
			t.Fatalf("run() relay deploy error = %v, want CLOUDFLARE_API_TOKEN guidance error", err)
		}
	})
	if want := "  Deploying relay for acme-net\n\n" + buildMissingCloudflareTokenGuidance("acme-net"); output != want {
		t.Fatalf("guidance output = %q, want the header followed by byte-identical buildMissingCloudflareTokenGuidance() = %q", output, want)
	}
}

// TestRunRelayDeployEndToEndUsesStoredTokenAndPrintsSourceLine is a full
// deploy through a fake Cloudflare server, proving the stored per-network
// token both authenticates the deploy and gets its source line printed —
// not just that the right string is chosen in isolation.
func TestRunRelayDeployEndToEndUsesStoredTokenAndPrintsSourceLine(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	if err := relaydeploy.SaveCloudflareToken(tokenPath, "stored-cf-token"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}

	fake := startFakeCloudflareCLIServer(t, "stored-cf-token")
	withRelayDeployFakeCloudflare(t, fake)

	// The stored-token source line is --verbose-only detail (quiet mode
	// collapses it away); this is the one test that actually proves it
	// still exists and names the right file.
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--verbose"}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, "using stored Cloudflare API token from "+tokenPath) {
		t.Fatalf("expected the stored-token source line naming %q, got %q", tokenPath, output)
	}
	if !strings.Contains(output, "relay live") {
		t.Fatalf("expected a successful deploy, got %q", output)
	}
	if strings.Contains(output, "stored-cf-token") {
		t.Fatalf("expected the token value itself never to be printed, got %q", output)
	}
}

// TestRunRelayDeployEndToEndEnvBeatsStoredToken proves precedence
// behaviorally: the fake server only accepts the env token, and a
// (deliberately different) token is already stored. If env did not win,
// authentication would fail against the fake server.
func TestRunRelayDeployEndToEndEnvBeatsStoredToken(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	if err := relaydeploy.SaveCloudflareToken(tokenPath, "stale-stored-token"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if strings.Contains(output, "using stored Cloudflare API token") {
		t.Fatalf("expected no stored-token source line when the env token wins, got %q", output)
	}
	// The stale stored token must be left exactly as it was: env winning
	// does not imply re-saving over it.
	if got := cloudflareTokenFileContents(t, tokenPath); got != "stale-stored-token" {
		t.Fatalf("stored token = %q, want unchanged stale-stored-token", got)
	}
}

// TestRunRelayDeployEndToEndSaveTokenFlag exercises --save-token end to
// end: a successful deploy using the env token, then persisted afterward.
func TestRunRelayDeployEndToEndSaveTokenFlag(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test"); err != nil {
			t.Fatalf("run() relay deploy --save-token error = %v", err)
		}
	})
	if got := cloudflareTokenFileContents(t, tokenPath); got != "env-cf-token" {
		t.Fatalf("saved token = %q, want env-cf-token", got)
	}
	if !strings.Contains(output, "saved Cloudflare API token to "+tokenPath) {
		t.Fatalf("expected a save confirmation naming %q, got %q", tokenPath, output)
	}
}

// TestRunRelayDeployEndToEndOfferPromptSavesOnYes exercises the TTY
// save-offer prompt end to end after a real (fake) successful deploy.
func TestRunRelayDeployEndToEndOfferPromptSavesOnYes(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)
	// P2 env-path save-offer gating fix: the offer now requires both stdin
	// AND stdout to be a real terminal (matching the pasted path's own
	// gate), not stdin alone — see withInteractiveOutput's doc comment.
	withInteractiveOutput(t)
	withPromptAnswers(t, "y")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, "save this token to "+tokenPath) {
		t.Fatalf("expected the save-offer prompt naming %q, got %q", tokenPath, output)
	}
	if got := cloudflareTokenFileContents(t, tokenPath); got != "env-cf-token" {
		t.Fatalf("saved token = %q, want env-cf-token", got)
	}
}

// TestRunRelayDeployEndToEndNonInteractiveSkipsOfferPrompt confirms a
// non-interactive successful deploy (the default in this test environment,
// and in CI/scripts) never prints the save offer and never saves without
// --save-token.
func TestRunRelayDeployEndToEndNonInteractiveSkipsOfferPrompt(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)

	previousInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = previousInteractive })

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if strings.Contains(output, "save this token") {
		t.Fatalf("expected no save-offer prompt when non-interactive, got %q", output)
	}
	if _, ok, err := relaydeploy.LoadCloudflareToken(tokenPath); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() = (ok=%v, err=%v), want (false, nil): non-interactive must never save without --save-token", ok, err)
	}
}

// writeCorruptCloudflareTokenFile writes unparseable JSON at tokenPath,
// simulating a stored Cloudflare token file damaged outside of Moltnet
// (partial write, disk corruption, manual edit gone wrong).
func writeCorruptCloudflareTokenFile(t *testing.T, tokenPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

// TestRunRelayDeployCorruptStoredTokenFileFallsBackToEnvToken covers the
// P2 fix: a corrupt/unreadable stored token file must not block a deploy
// that has a working CLOUDFLARE_API_TOKEN env override. It should warn
// (naming the file) and proceed using the env token.
func TestRunRelayDeployCorruptStoredTokenFileFallsBackToEnvToken(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	writeCorruptCloudflareTokenFile(t, tokenPath)

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v, want the corrupt stored file not to block a deploy using the env token", err)
		}
	})
	if !strings.Contains(output, tokenPath) {
		t.Fatalf("expected a warning naming the corrupt stored token file %q, got %q", tokenPath, output)
	}
	if !strings.Contains(output, "relay live") {
		t.Fatalf("expected a successful deploy using the env token, got %q", output)
	}
}

// TestRunRelayDeployCorruptStoredTokenFileWithNoEnvTokenSurfacesError covers
// the other half of the P2 fix: with no CLOUDFLARE_API_TOKEN to fall back
// to, the corrupt stored file's load error must surface, naming the file
// and suggesting --forget-token.
func TestRunRelayDeployCorruptStoredTokenFileWithNoEnvTokenSurfacesError(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	writeCorruptCloudflareTokenFile(t, tokenPath)

	err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test")
	if err == nil {
		t.Fatal("run() relay deploy error = nil, want the corrupt stored token load error to surface")
	}
	if !strings.Contains(err.Error(), tokenPath) {
		t.Fatalf("run() relay deploy error = %v, want it to name %q", err, tokenPath)
	}
	if !strings.Contains(err.Error(), "--forget-token") {
		t.Fatalf("run() relay deploy error = %v, want it to suggest --forget-token", err)
	}
}

// TestRunRelayDeployCorruptStoredTokenFileInteractiveFallsThroughToPrompt
// covers the P3-12 fix: on an interactive terminal, a corrupt/unreadable
// stored token file with no CLOUDFLARE_API_TOKEN to fall back to must not
// be an early hard error (that's the non-interactive behavior, pinned by
// TestRunRelayDeployCorruptStoredTokenFileWithNoEnvTokenSurfacesError above,
// unchanged) — it must warn and fall through to the interactive paste
// prompt, exactly like having nothing stored at all.
func TestRunRelayDeployCorruptStoredTokenFileInteractiveFallsThroughToPrompt(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	writeCorruptCloudflareTokenFile(t, tokenPath)

	fake := startFakeCloudflareCLIServer(t, "pasted-cf-token")
	withRelayDeployFakeCloudflare(t, fake)
	withInteractiveOutput(t)
	withPromptAnswers(t, "pasted-cf-token", "n")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v, want the corrupt stored file to fall through to the interactive prompt instead of erroring", err)
		}
	})
	if !strings.Contains(output, tokenPath) {
		t.Fatalf("expected a warning naming the corrupt stored token file %q, got %q", tokenPath, output)
	}
	if !strings.Contains(output, "paste token (input hidden): ") {
		t.Fatalf("expected the interactive paste prompt to have been reached, got %q", output)
	}
	if !strings.Contains(output, "relay live") {
		t.Fatalf("expected a successful deploy using the pasted token, got %q", output)
	}
}

// TestRunRelayDeployEndToEndSaveTokenFlagWithStoredTokenOnlyPrintsNothingToSave
// covers the P3 fix: --save-token when the deploy already used the stored
// token (nothing new to persist) must say so instead of silently doing
// nothing.
func TestRunRelayDeployEndToEndSaveTokenFlagWithStoredTokenOnlyPrintsNothingToSave(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	if err := relaydeploy.SaveCloudflareToken(tokenPath, "stored-cf-token"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}

	fake := startFakeCloudflareCLIServer(t, "stored-cf-token")
	withRelayDeployFakeCloudflare(t, fake)

	// The nothing-to-save note is --verbose-only (informational, not
	// actionable); this is the one test proving it still exists.
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token", "--verbose"}, "test"); err != nil {
			t.Fatalf("run() relay deploy --save-token error = %v", err)
		}
	})
	if !strings.Contains(output, "token already stored at "+tokenPath+"; nothing to save") {
		t.Fatalf("expected a nothing-to-save message naming %q, got %q", tokenPath, output)
	}
}

// TestRunRelayDeploySaveTokenAndForgetTokenConflict covers the P3 fix:
// --save-token and --forget-token together must fail with a flag-conflict
// error before any deploy or token I/O is attempted.
func TestRunRelayDeploySaveTokenAndForgetTokenConflict(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")

	err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token", "--forget-token"}, "test")
	if err == nil {
		t.Fatal("run() relay deploy --save-token --forget-token error = nil, want a flag-conflict error")
	}
	if !strings.Contains(err.Error(), "--save-token") || !strings.Contains(err.Error(), "--forget-token") {
		t.Fatalf("run() relay deploy --save-token --forget-token error = %v, want it to name both flags", err)
	}
}
