package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func writeMoltnetConfig(t *testing.T, directory string, networkID string, networkName string) string {
	t.Helper()

	path := filepath.Join(directory, app.DefaultPath)
	contents := "version: moltnet.v1\n\nnetwork:\n  id: " + networkID + "\n  name: " + networkName + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write Moltnet config: %v", err)
	}
	return path
}

func extractInviteCode(t *testing.T, output string) string {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, protocol.InvitePrefix) {
			return line
		}
	}
	t.Fatalf("no invite code found in output:\n%s", output)
	return ""
}

func TestRunPairInviteWritesConfigAndPrintsCode(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--room", "chat",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})

	if !strings.Contains(output, "wrote pairing \"friend-net\"") {
		t.Fatalf("unexpected output %q", output)
	}
	if !strings.Contains(output, "restart the Moltnet server") {
		t.Fatalf("expected restart reminder in output %q", output)
	}

	code := extractInviteCode(t, output)
	invite, err := protocol.DecodeInvite(code)
	if err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}
	if invite.NetworkID != "alice-net" || invite.NetworkName != "Alice's Moltnet" {
		t.Fatalf("unexpected invite identity %#v", invite)
	}
	if invite.RelayURL != "wss://moltnet-relay.acme.workers.dev" || invite.RelayToken != "relay-secret-value" {
		t.Fatalf("unexpected invite relay fields %#v", invite)
	}
	if invite.PairingID != "friend-net" {
		t.Fatalf("invite pairing id = %q, want friend-net", invite.PairingID)
	}
	if len(invite.SharedRooms) != 1 || invite.SharedRooms[0] != "chat" {
		t.Fatalf("unexpected invite shared_rooms %#v", invite.SharedRooms)
	}

	config, err := app.LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Pairings) != 1 || config.Pairings[0].ID != "friend-net" {
		t.Fatalf("unexpected pairings %#v", config.Pairings)
	}
	if config.Pairings[0].Relay == nil || config.Pairings[0].Relay.Token.Reveal() != "relay-secret-value" {
		t.Fatalf("unexpected pairing relay %#v", config.Pairings[0].Relay)
	}
	if config.Pairings[0].Token.Reveal() != invite.PairingToken {
		t.Fatalf("local pairing token does not match invite pairing token")
	}
	if len(config.Auth.Tokens) != 1 || config.Auth.Tokens[0].Value != invite.PairingToken {
		t.Fatalf("unexpected auth tokens %#v", config.Auth.Tokens)
	}
}

func TestRunPairInviteRefusesDefaultNetworkID(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, app.DefaultNetworkID, app.DefaultNetworkName)

	err := run(context.Background(), []string{
		"pair", "invite",
		"--relay-url", "wss://moltnet-relay.acme.workers.dev",
		"--relay-token-env", "TEST_RELAY_TOKEN",
		"--config", path,
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("run() pair invite error = %v, want default network id refusal", err)
	}
}

func TestRunPairInviteRequiresRelayFlags(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	if err := run(context.Background(), []string{"pair", "invite", "--config", path}, "test"); err == nil {
		t.Fatal("expected error for missing --relay-url")
	}

	err := run(context.Background(), []string{
		"pair", "invite", "--relay-url", "wss://relay.example.dev", "--config", path,
	}, "test")
	if err == nil {
		t.Fatal("expected error for missing --relay-token-env")
	}
}

func TestRunPairInvitePrintOnlyDoesNotWriteConfig(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config before: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://relay.example.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--print-only",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite --print-only error = %v", err)
		}
	})

	code := extractInviteCode(t, output)
	if _, err := protocol.DecodeInvite(code); err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("--print-only wrote to config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRunPairJoinWritesMirroredPairing(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	aliceDir := t.TempDir()
	alicePath := writeMoltnetConfig(t, aliceDir, "alice-net", "Alice's Moltnet")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--config", alicePath,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)
	invite, err := protocol.DecodeInvite(code)
	if err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}

	bobDir := t.TempDir()
	bobPath := writeMoltnetConfig(t, bobDir, "bob-net", "Bob's Moltnet")

	joinOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test"); err != nil {
			t.Fatalf("run() pair <code> error = %v", err)
		}
	})
	if !strings.Contains(joinOutput, "friend-net") || !strings.Contains(joinOutput, "alice-net") {
		t.Fatalf("unexpected join output %q", joinOutput)
	}

	bobConfig, err := app.LoadConfigForPath(bobPath, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(bobConfig.Pairings) != 1 {
		t.Fatalf("unexpected pairings %#v", bobConfig.Pairings)
	}
	pairing := bobConfig.Pairings[0]
	if pairing.ID != "friend-net" || pairing.RemoteNetworkID != "alice-net" || pairing.RemoteNetworkName != "Alice's Moltnet" {
		t.Fatalf("unexpected mirrored pairing %#v", pairing)
	}
	if pairing.Token.Reveal() != invite.PairingToken {
		t.Fatalf("mirrored pairing token does not match invite pairing token")
	}
	if pairing.Relay == nil || pairing.Relay.URL != invite.RelayURL || pairing.Relay.Room != invite.Room ||
		pairing.Relay.Token.Reveal() != invite.RelayToken {
		t.Fatalf("unexpected mirrored pairing relay %#v", pairing.Relay)
	}
	if len(bobConfig.Auth.Tokens) != 1 || bobConfig.Auth.Tokens[0].Value != invite.PairingToken ||
		bobConfig.Auth.Tokens[0].Network != "alice-net" {
		t.Fatalf("unexpected mirrored auth token %#v", bobConfig.Auth.Tokens)
	}
}

func TestRunPairJoinRefusesNetworkIDCollision(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	aliceDir := t.TempDir()
	alicePath := writeMoltnetConfig(t, aliceDir, "shared-net", "Shared")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--config", alicePath,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	bobDir := t.TempDir()
	bobPath := writeMoltnetConfig(t, bobDir, "shared-net", "Also Shared")

	err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test")
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("run() pair <code> error = %v, want network id collision refusal", err)
	}
}

func TestRunPairJoinRefusesDuplicateWithoutForce(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	aliceDir := t.TempDir()
	alicePath := writeMoltnetConfig(t, aliceDir, "alice-net", "Alice's Moltnet")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--config", alicePath,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	bobDir := t.TempDir()
	bobPath := writeMoltnetConfig(t, bobDir, "bob-net", "Bob's Moltnet")
	if err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test"); err != nil {
		t.Fatalf("first run() pair <code> error = %v", err)
	}

	err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("run() pair <code> error = %v, want duplicate refusal", err)
	}

	if err := run(context.Background(), []string{"pair", code, "--config", bobPath, "--force"}, "test"); err != nil {
		t.Fatalf("run() pair <code> --force error = %v", err)
	}

	bobConfig, err := app.LoadConfigForPath(bobPath, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(bobConfig.Pairings) != 1 {
		t.Fatalf("expected --force to keep a single pairing, got %#v", bobConfig.Pairings)
	}
}

func TestRunPairJoinRejectsExpiredInvite(t *testing.T) {
	invite := protocol.Invite{
		RelayURL:     "wss://relay.example.dev",
		Room:         "room-1",
		RelayToken:   "relay-token",
		PairingToken: "pairing-token",
		PairingID:    "friend-net",
		NetworkID:    "alice-net",
	}
	// Leave Exp zero to trigger the "invite exp is required" validation path,
	// covering DecodeInvite's error surfacing through the pair command.
	code, err := protocol.EncodeInvite(invite)
	if err != nil {
		t.Fatalf("EncodeInvite() error = %v", err)
	}

	bobDir := t.TempDir()
	bobPath := writeMoltnetConfig(t, bobDir, "bob-net", "Bob's Moltnet")

	if err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test"); err == nil {
		t.Fatal("expected error for invite with no expiry")
	}
}

// TestWritePairingWithRollbackRestoresConfigOnReloadFailure covers
// writePairingWithRollback's rollback path directly: a pairing that
// declares neither remote_base_url nor relay passes WritePairing's own
// structural write (it does not validate that constraint), but fails the
// post-write LoadConfigForPath reload check, which does. The write also
// upserts a shared room, so this exercises the rollback restoring the
// pairing, auth token, and room together, byte-for-byte.
func TestWritePairingWithRollbackRestoresConfigOnReloadFailure(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config before: %v", err)
	}

	pairing := app.PairingWriteback{ID: "friend-net", RemoteNetworkID: "bob-net", Token: "pairing-token"}
	authToken := app.AuthTokenWriteback{ID: "friend-net", Value: "pairing-token", Scopes: []string{"pair"}}

	err = writePairingWithRollback(path, pairing, authToken, []string{"chat"}, false)
	if err == nil {
		t.Fatal("expected writePairingWithRollback to fail the post-write reload")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected error to mention the rollback, got %q", err.Error())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("config was not restored byte-for-byte after a failed reload:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRunPairCommandRequiresArguments(t *testing.T) {
	if err := run(context.Background(), []string{"pair"}, "test"); err == nil {
		t.Fatal("expected error when pair is called without arguments")
	}
}

func TestRunPairHelp(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"pair", "help"}, "test"); err != nil {
			t.Fatalf("run() pair help error = %v", err)
		}
	})
	if !strings.Contains(output, "moltnet pair invite") {
		t.Fatalf("unexpected pair help output %q", output)
	}
}

func TestRunPairErrorsWhenConfigMissing(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	missing := filepath.Join(directory, "Moltnet")

	err := run(context.Background(), []string{
		"pair", "invite",
		"--relay-url", "wss://relay.example.dev",
		"--relay-token-env", "TEST_RELAY_TOKEN",
		"--config", missing,
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "moltnet init") {
		t.Fatalf("run() pair invite error = %v, want config-not-found guidance", err)
	}
}
