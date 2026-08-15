package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// These tests cover the per-network stored Cloudflare API token: resolution
// precedence, --save-token, the TTY save-offer prompt, and --forget-token.
// Full end-to-end deploys go through a fakeCloudflareCLIServer
// (relay_deploy_cloudflare_test.go); pure resolution/save-decision logic is
// tested directly against resolveCloudflareAPIToken and
// maybeSaveCloudflareToken.

func TestResolveCloudflareAPITokenEnvBeatsStored(t *testing.T) {
	token, source := resolveCloudflareAPIToken("env-token", "stored-token", true)
	if token != "env-token" || source != cloudflareTokenSourceEnv {
		t.Fatalf("resolveCloudflareAPIToken() = (%q, %q), want (env-token, %q)", token, source, cloudflareTokenSourceEnv)
	}
}

func TestResolveCloudflareAPITokenStoredBeatsMissing(t *testing.T) {
	token, source := resolveCloudflareAPIToken("", "stored-token", true)
	if token != "stored-token" || source != cloudflareTokenSourceStored {
		t.Fatalf("resolveCloudflareAPIToken() = (%q, %q), want (stored-token, %q)", token, source, cloudflareTokenSourceStored)
	}
}

func TestResolveCloudflareAPITokenNeitherIsEmpty(t *testing.T) {
	token, source := resolveCloudflareAPIToken("", "", false)
	if token != "" || source != "" {
		t.Fatalf("resolveCloudflareAPIToken() = (%q, %q), want (\"\", \"\")", token, source)
	}
}

func TestResolveCloudflareAPITokenBlankEnvFallsBackToStored(t *testing.T) {
	// os.Getenv on an unset var, and a var explicitly set to whitespace,
	// both look like "" after TrimSpace; either way stored must still win.
	token, source := resolveCloudflareAPIToken("   ", "stored-token", true)
	if token != "stored-token" || source != cloudflareTokenSourceStored {
		t.Fatalf("resolveCloudflareAPIToken() = (%q, %q), want (stored-token, %q)", token, source, cloudflareTokenSourceStored)
	}
}

func TestMaybeSaveCloudflareTokenSavesWithFlag(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	output := captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(tokenPath, "cf-token", true, false); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if got := cloudflareTokenFileContents(t, tokenPath); got != "cf-token" {
		t.Fatalf("saved token = %q, want cf-token", got)
	}
	if !strings.Contains(output, tokenPath) {
		t.Fatalf("expected save confirmation naming %q, got %q", tokenPath, output)
	}
	if strings.Contains(output, "cf-token") {
		t.Fatalf("expected the token value itself never to be printed, got %q", output)
	}
}

func TestMaybeSaveCloudflareTokenPromptYesSaves(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	withPromptAnswers(t, "y")
	output := captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(tokenPath, "cf-token", false, false); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if got := cloudflareTokenFileContents(t, tokenPath); got != "cf-token" {
		t.Fatalf("saved token = %q, want cf-token", got)
	}
	if !strings.Contains(output, "save this token to "+tokenPath) {
		t.Fatalf("expected the save-offer prompt naming %q, got %q", tokenPath, output)
	}
}

func TestMaybeSaveCloudflareTokenPromptNoSkipsSave(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	withPromptAnswers(t, "n")
	captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(tokenPath, "cf-token", false, false); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if _, ok, err := relaydeploy.LoadCloudflareToken(tokenPath); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() after declining = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestMaybeSaveCloudflareTokenNonInteractiveNeverPrompts(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	previousInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = previousInteractive })

	output := captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(tokenPath, "cf-token", false, false); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if strings.Contains(output, "save this token") {
		t.Fatalf("expected no save-offer prompt when non-interactive, got %q", output)
	}
	if _, ok, err := relaydeploy.LoadCloudflareToken(tokenPath); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() = (ok=%v, err=%v), want (false, nil): non-interactive must never save without --save-token", ok, err)
	}
}

func TestMaybeSaveCloudflareTokenSkipsOfferWhenAlreadyStored(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	previousInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = previousInteractive })

	// storedTokenOK=true means something is already stored; the offer-once
	// prompt must not re-ask (and promptReader is left untouched, so a
	// prompt attempt here would hang reading EOF from os.Stdin — proof it
	// was skipped, not just declined).
	output := captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(tokenPath, "cf-token", false, true); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if strings.Contains(output, "save this token") {
		t.Fatalf("expected no save-offer prompt when a token is already stored, got %q", output)
	}
}

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
	if output != buildMissingCloudflareTokenGuidance("acme-net") {
		t.Fatalf("guidance output = %q, want byte-identical to buildMissingCloudflareTokenGuidance()", output)
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

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v", err)
		}
	})
	if !strings.Contains(output, "using stored Cloudflare API token from "+tokenPath) {
		t.Fatalf("expected the stored-token source line naming %q, got %q", tokenPath, output)
	}
	if !strings.Contains(output, `deployed relay Worker "moltnet-relay"`) {
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
	if !strings.Contains(output, `deployed relay Worker "moltnet-relay"`) {
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

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path, "--save-token"}, "test"); err != nil {
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
