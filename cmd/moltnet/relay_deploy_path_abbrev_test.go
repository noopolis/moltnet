package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// These tests cover the P2-3 fix: two operator-facing messages that used to
// leak a raw absolute path amid otherwise ~-abbreviated output — the
// corrupt-stored-token warning (relay_deploy.go, wrapping
// relaydeploy.LoadCloudflareToken's own decode error) and the
// rejected-stored-token error (relay_deploy.go, wrapping
// internal/relaydeploy's wrapStoredTokenError). Both are fixed at the
// presentation layer (abbreviatePathInMessage / abbreviatePathError,
// relay_deploy.go) rather than in internal/relaydeploy itself — that
// package's own tests (deploy_test.go) still pin the raw path in what it
// returns to its callers — so both tests here set HOME to a real temp
// directory (not just t.TempDir(), which never sits under HOME) to actually
// exercise abbreviateHome's rewrite, matching the pattern
// pair_aftercare_test.go already uses for the same reason.

func TestRunRelayDeployCorruptStoredTokenWarningAbbreviatesPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	directory := filepath.Join(home, ".moltnet", "acme-net")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("mkdir network directory: %v", err)
	}
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("mkdir .moltnet: %v", err)
	}
	// Not valid JSON: relaydeploy.LoadCloudflareToken's decode error embeds
	// tokenPath verbatim (cloudflare_token.go), the raw absolute path this
	// fix must abbreviate before printing.
	if err := os.WriteFile(tokenPath, []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt stored token file: %v", err)
	}

	fake := startFakeCloudflareCLIServer(t, "env-cf-token")
	withRelayDeployFakeCloudflare(t, fake)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test"); err != nil {
			t.Fatalf("run() relay deploy error = %v (want the corrupt stored token to only warn, not block, since CLOUDFLARE_API_TOKEN is set)", err)
		}
	})
	if strings.Contains(output, home) {
		t.Fatalf("expected the corrupt-token warning to abbreviate the home-relative path, got %q", output)
	}
	if !strings.Contains(output, "decode stored Cloudflare API token") {
		t.Fatalf("expected the warning to still name the underlying decode failure, got %q", output)
	}
	if !strings.Contains(output, "~"+string(filepath.Separator)+".moltnet"+string(filepath.Separator)+"acme-net"+string(filepath.Separator)+".moltnet"+string(filepath.Separator)+"cloudflare.json") {
		t.Fatalf("expected the warning to name the ~-abbreviated token path, got %q", output)
	}
}

func TestRunRelayDeployRejectedStoredTokenAbbreviatesPathInError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := filepath.Join(home, ".moltnet", "acme-net")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("mkdir network directory: %v", err)
	}
	path := writeMoltnetConfig(t, directory, "acme-net", "Acme Net")
	tokenPath := relaydeploy.CloudflareTokenPath(path)
	if err := relaydeploy.SaveCloudflareToken(tokenPath, "stale-stored-token"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}

	// The fake only accepts "the-real-token", so authenticating with the
	// stored (stale) token 401s — relaydeploy.Deploy wraps that as the
	// rejected-stored-token error naming tokenPath.
	fake := startFakeCloudflareCLIServer(t, "the-real-token")
	withRelayDeployFakeCloudflare(t, fake)

	var err error
	captureStdout(t, func() {
		err = run(context.Background(), []string{"relay", "deploy", "--config", path}, "test")
	})
	if err == nil {
		t.Fatal("run() relay deploy error = nil, want the rejected-stored-token error")
	}
	if strings.Contains(err.Error(), home) {
		t.Fatalf("run() relay deploy error = %v, want the home-relative path abbreviated", err)
	}
	if !strings.Contains(err.Error(), "~"+string(filepath.Separator)+".moltnet"+string(filepath.Separator)+"acme-net"+string(filepath.Separator)+".moltnet"+string(filepath.Separator)+"cloudflare.json") {
		t.Fatalf("run() relay deploy error = %v, want it to name the ~-abbreviated stored token path", err)
	}
	if !strings.Contains(err.Error(), "--forget-token") {
		t.Fatalf("run() relay deploy error = %v, want it to still suggest --forget-token", err)
	}
}
