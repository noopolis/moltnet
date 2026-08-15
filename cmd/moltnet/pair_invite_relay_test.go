package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/relaydeploy"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestRunPairInviteUsesStoredRelayCredentialsWhenFlagsOmitted covers
// PLAN.md's "pair invite gains the zero-flag path": with no
// --relay-url/--relay-token-env, it should read .moltnet/relay.json next to
// the Moltnet config, as `moltnet relay deploy` would have written it.
func TestRunPairInviteUsesStoredRelayCredentialsWhenFlagsOmitted(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")
	credentialsPath := relaydeploy.CredentialsPath(path)
	if err := relaydeploy.SaveCredentials(credentialsPath, relaydeploy.RelayCredentials{
		URL:   "wss://moltnet-relay.acme.workers.dev",
		Token: "stored-relay-token",
	}); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--id", "friend-net",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})

	code := extractInviteCode(t, output)
	invite, err := protocol.DecodeInvite(code)
	if err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}
	if invite.RelayURL != "wss://moltnet-relay.acme.workers.dev" {
		t.Fatalf("invite.RelayURL = %q, want the stored relay url", invite.RelayURL)
	}
	if invite.RelayToken != "stored-relay-token" {
		t.Fatalf("invite.RelayToken = %q, want the stored relay token", invite.RelayToken)
	}
}

// TestRunPairInviteFlagsOverrideStoredRelayCredentials covers "flags still
// override": a --relay-url given alongside stored credentials wins for the
// URL, while the token still falls back to the stored value.
func TestRunPairInviteFlagsOverrideStoredRelayCredentials(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")
	credentialsPath := relaydeploy.CredentialsPath(path)
	if err := relaydeploy.SaveCredentials(credentialsPath, relaydeploy.RelayCredentials{
		URL:   "wss://stored-relay.acme.workers.dev",
		Token: "stored-relay-token",
	}); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://override-relay.acme.workers.dev",
			"--id", "friend-net",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})

	code := extractInviteCode(t, output)
	invite, err := protocol.DecodeInvite(code)
	if err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}
	if invite.RelayURL != "wss://override-relay.acme.workers.dev" {
		t.Fatalf("invite.RelayURL = %q, want the --relay-url override", invite.RelayURL)
	}
	if invite.RelayToken != "stored-relay-token" {
		t.Fatalf("invite.RelayToken = %q, want the stored relay token to fill in the unset half", invite.RelayToken)
	}
}

// TestRunPairInviteRequiresRelayCredentialsWhenNoneAvailable covers the
// case where neither flags nor a stored .moltnet/relay.json exist.
func TestRunPairInviteRequiresRelayCredentialsWhenNoneAvailable(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	err := run(context.Background(), []string{"pair", "invite", "--config", path}, "test")
	if err == nil || !strings.Contains(err.Error(), "moltnet relay deploy") {
		t.Fatalf("run() pair invite error = %v, want guidance to run `moltnet relay deploy`", err)
	}
}

// TestRunPairInviteRelayTokenEnvOverridesStoredToken covers overriding just
// the token half while the URL falls back to storage.
func TestRunPairInviteRelayTokenEnvOverridesStoredToken(t *testing.T) {
	t.Setenv("TEST_OVERRIDE_RELAY_TOKEN", "override-relay-token")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")
	credentialsPath := relaydeploy.CredentialsPath(path)
	if err := relaydeploy.SaveCredentials(credentialsPath, relaydeploy.RelayCredentials{
		URL:   "wss://stored-relay.acme.workers.dev",
		Token: "stored-relay-token",
	}); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-token-env", "TEST_OVERRIDE_RELAY_TOKEN",
			"--id", "friend-net",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})

	code := extractInviteCode(t, output)
	invite, err := protocol.DecodeInvite(code)
	if err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}
	if invite.RelayURL != "wss://stored-relay.acme.workers.dev" {
		t.Fatalf("invite.RelayURL = %q, want the stored relay url to fill in the unset half", invite.RelayURL)
	}
	if invite.RelayToken != "override-relay-token" {
		t.Fatalf("invite.RelayToken = %q, want the --relay-token-env override", invite.RelayToken)
	}
}

func TestResolvePairInviteRelayCredentialsDecodeError(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, app.DefaultPath)
	credentialsPath := relaydeploy.CredentialsPath(path)
	if err := relaydeploy.SaveCredentials(credentialsPath, relaydeploy.RelayCredentials{URL: "wss://x", Token: "y"}); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	relayURL, relayToken, err := resolvePairInviteRelayCredentials(path, "acme-net", "", "")
	if err != nil {
		t.Fatalf("resolvePairInviteRelayCredentials() error = %v", err)
	}
	if relayURL != "wss://x" || relayToken != "y" {
		t.Fatalf("resolvePairInviteRelayCredentials() = (%q, %q), want (wss://x, y)", relayURL, relayToken)
	}
}
