package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/service"
)

func TestRunPairInvitePrintsMembershipCommandWithPlaceholderRemote(t *testing.T) {
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

	if !strings.Contains(output, "moltnet admin room members add --room chat") {
		t.Fatalf("expected a membership command in output %q", output)
	}
	if !strings.Contains(output, "--member <friend-network-id>:<remote-member-id>") {
		t.Fatalf("expected a placeholder remote network id in output %q", output)
	}
}

func TestRunPairJoinPrintsMembershipCommandWithRealRemoteNetworkID(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	aliceDir := t.TempDir()
	alicePath := writeMoltnetConfig(t, aliceDir, "alice-net", "Alice's Moltnet")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--room", "chat",
			"--config", alicePath,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	bobDir := t.TempDir()
	bobPath := writeMoltnetConfig(t, bobDir, "bob-net", "Bob's Moltnet")

	joinOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test"); err != nil {
			t.Fatalf("run() pair join error = %v", err)
		}
	})

	if !strings.Contains(joinOutput, "moltnet admin room members add --room chat --member alice-net:<remote-member-id>") {
		t.Fatalf("expected a membership command naming alice-net in output %q", joinOutput)
	}
}

func TestRunPairJoinRestartErrorsWithoutManagedService(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	t.Setenv("HOME", t.TempDir())

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

	err := run(context.Background(), []string{"pair", code, "--config", bobPath, "--restart"}, "test")
	if err == nil {
		t.Fatal("expected --restart to error when no service is installed for bob-net")
	}
	if !strings.Contains(err.Error(), "restart") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRunPairJoinRestartSucceedsWithManagedService(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	home := t.TempDir()
	t.Setenv("HOME", home)
	withFakeServiceManager(t, "linux")

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

	// Pre-install a fake-runner-backed service for bob-net so --restart has
	// something to find.
	manager := service.NewForOS(fakeServiceRunner{}, "linux")
	spec := service.Spec{
		NetworkID:  "bob-net",
		ConfigPath: bobPath,
		BinaryPath: filepath.Join(bobDir, "moltnet"),
		NetworkDir: bobDir,
	}
	if err := manager.Install(context.Background(), spec); err != nil {
		t.Fatalf("pre-install fake service: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"pair", code, "--config", bobPath, "--restart"}, "test"); err != nil {
			t.Fatalf("run() pair join --restart error = %v", err)
		}
	})
	if !strings.Contains(output, "restarted the moltnet service for network \"bob-net\"") {
		t.Fatalf("expected restart confirmation in output %q", output)
	}
}
