package main

import (
	"context"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
)

// These tests cover the parts of `moltnet relay deploy` that do not touch
// the network: flag parsing, --print-manual, and the guidance printed when
// CLOUDFLARE_API_TOKEN or --token-env are missing/empty. The Cloudflare
// client itself is covered against net/http/httptest mocks in
// internal/relaydeploy.

func TestRunRelayDeployPrintManualDoesNotRequireCloudflareToken(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "deploy", "--name", "custom-relay", "--print-manual"}, "test"); err != nil {
			t.Fatalf("run() relay deploy --print-manual error = %v", err)
		}
	})
	if !strings.Contains(output, "wrangler deploy --name custom-relay") {
		t.Fatalf("unexpected --print-manual output %q", output)
	}
	if !strings.Contains(output, "wrangler secret put RELAY_TOKEN") {
		t.Fatalf("expected --print-manual output to mention the RELAY_TOKEN secret, got %q", output)
	}
}

func TestRunRelayDeployRequiresCloudflareAPIToken(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{"relay", "deploy", "--config", path}, "test")
		if err == nil || !strings.Contains(err.Error(), "CLOUDFLARE_API_TOKEN") {
			t.Fatalf("run() relay deploy error = %v, want a CLOUDFLARE_API_TOKEN guidance error", err)
		}
	})
	if !strings.Contains(output, "dash.cloudflare.com") {
		t.Fatalf("expected dashboard guidance in output, got %q", output)
	}
	if !strings.Contains(output, "moltnet relay deploy --id alice-net") {
		t.Fatalf("expected the rerun command to name this network's --id, got %q", output)
	}
}

// TestBuildMissingCloudflareTokenGuidanceNamesNetworkID and
// TestBuildWorkersDevSubdomainGuidanceNamesNetworkID pin `--id %s` in both
// relay-deploy guidance builders directly, independent of the harder-to-
// reach end-to-end paths (the CLOUDFLARE_API_TOKEN guidance is covered
// end-to-end above; the workers.dev subdomain guidance only prints after a
// real Cloudflare deploy call reaches relaydeploy.ErrWorkersDevSubdomainUnclaimed,
// which is not reachable from this package's tests without a live API).
func TestBuildMissingCloudflareTokenGuidanceNamesNetworkID(t *testing.T) {
	guidance := buildMissingCloudflareTokenGuidance("acme-net")
	if !strings.Contains(guidance, "moltnet relay deploy --id acme-net") {
		t.Fatalf("expected guidance to name --id acme-net, got %q", guidance)
	}
}

// TestBuildCloudflareTokenDeepLinkExactURL pins the exact encoded deep link
// against Cloudflare's documented user-token template URL format
// (permissionGroupKeys=<url-encoded JSON>&accountId=*&zoneId=all&name=<name>,
// see developers.cloudflare.com/fundamentals/api/how-to/account-owned-token-template/),
// with the single Account > Workers Scripts > Edit permission group this
// deploy flow actually needs.
func TestBuildCloudflareTokenDeepLinkExactURL(t *testing.T) {
	got := buildCloudflareTokenDeepLink("moltnet-relay-deploy")
	want := "https://dash.cloudflare.com/profile/api-tokens" +
		"?permissionGroupKeys=%5B%7B%22key%22%3A%22workers_scripts%22%2C%22type%22%3A%22edit%22%7D%5D" +
		"&accountId=%2A&zoneId=all&name=moltnet-relay-deploy"
	if got != want {
		t.Fatalf("buildCloudflareTokenDeepLink() = %q, want %q", got, want)
	}
}

// TestBuildMissingCloudflareTokenGuidanceIncludesDeepLink pins the deep link
// and its plain-dashboard fallback both appearing in the printed guidance.
func TestBuildMissingCloudflareTokenGuidanceIncludesDeepLink(t *testing.T) {
	guidance := buildMissingCloudflareTokenGuidance("acme-net")
	if !strings.Contains(guidance, buildCloudflareTokenDeepLink(cloudflareTokenTemplateName)) {
		t.Fatalf("expected guidance to include the Cloudflare token deep link, got %q", guidance)
	}
	if !strings.Contains(guidance, "  Or create one manually at:\n    "+cloudflareDashboardTokenURL) {
		t.Fatalf("expected guidance to keep the plain dashboard URL as a fallback, got %q", guidance)
	}
}

func TestBuildWorkersDevSubdomainGuidanceNamesNetworkID(t *testing.T) {
	guidance := buildWorkersDevSubdomainGuidance("acme-net")
	if !strings.Contains(guidance, "moltnet relay deploy --id acme-net") {
		t.Fatalf("expected guidance to name --id acme-net, got %q", guidance)
	}
}

// TestPrintRelayDeployNextStepsDefaultNetworkIDSuggestsReinit and
// TestPrintRelayDeployNextStepsNamesPairInviteForRealNetworkID cover the
// two branches of printRelayDeployNextSteps (relay_deploy.go), split out of
// runRelayDeploy specifically so the DefaultNetworkID re-init branch can be
// exercised without a real Cloudflare deploy.
func TestPrintRelayDeployNextStepsDefaultNetworkIDSuggestsReinit(t *testing.T) {
	output := captureStdout(t, func() {
		printRelayDeployNextSteps(app.DefaultNetworkID)
	})
	if !strings.Contains(output, "moltnet init --id <network-id>") {
		t.Fatalf("expected a re-init nudge for the default network id, got %q", output)
	}
	if strings.Contains(output, "pair invite") {
		t.Fatalf("expected no pair invite suggestion for the default network id (it would always fail), got %q", output)
	}
}

func TestPrintRelayDeployNextStepsNamesPairInviteForRealNetworkID(t *testing.T) {
	output := captureStdout(t, func() {
		printRelayDeployNextSteps("acme-net")
	})
	if !strings.Contains(output, "moltnet pair invite --network-id acme-net --room chat") {
		t.Fatalf("expected a filled-in pair invite command, got %q", output)
	}
}

func TestRunRelayDeployRequiresConfigWhenTokenIsSet(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "test-cloudflare-token")
	directory := t.TempDir()
	missing := directory + "/Moltnet"

	err := run(context.Background(), []string{"relay", "deploy", "--config", missing}, "test")
	if err == nil || !strings.Contains(err.Error(), "moltnet init") {
		t.Fatalf("run() relay deploy error = %v, want config-not-found guidance", err)
	}
}

// TestRunRelayDeployRejectsEmptyTokenEnvValue also covers the P3 fix moving
// this validation ahead of the "Deploying relay for <id>" header: an
// empty/unset --token-env value is a config-independent usage error, so it
// must return before that header ever prints — printing it first would
// suggest a deploy attempt actually started.
func TestRunRelayDeployRejectsEmptyTokenEnvValue(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "test-cloudflare-token")
	t.Setenv("TEST_EMPTY_RELAY_TOKEN", "")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{
			"relay", "deploy", "--config", path, "--token-env", "TEST_EMPTY_RELAY_TOKEN",
		}, "test")
	})
	if err == nil || !strings.Contains(err.Error(), "TEST_EMPTY_RELAY_TOKEN") {
		t.Fatalf("run() relay deploy error = %v, want empty --token-env guidance", err)
	}
	if output != "" {
		t.Fatalf("expected no output (no header) before this immediate usage error, got %q", output)
	}
}

func TestRunRelayCommandRequiresSubcommand(t *testing.T) {
	if err := run(context.Background(), []string{"relay"}, "test"); err == nil {
		t.Fatal("expected error when relay is called without a subcommand")
	}
}

func TestRunRelayCommandRejectsUnknownSubcommand(t *testing.T) {
	err := run(context.Background(), []string{"relay", "bogus"}, "test")
	if err == nil || !strings.Contains(err.Error(), "unknown relay subcommand") {
		t.Fatalf("run() relay bogus error = %v, want unknown-subcommand error", err)
	}
}

func TestRunRelayHelp(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"relay", "help"}, "test"); err != nil {
			t.Fatalf("run() relay help error = %v", err)
		}
	})
	if !strings.Contains(output, "moltnet relay deploy") {
		t.Fatalf("unexpected relay help output %q", output)
	}
}
