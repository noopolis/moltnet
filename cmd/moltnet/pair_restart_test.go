package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/service"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// failingRestartServiceRunner behaves exactly like fakeServiceRunner
// (service_test.go) for every command except a systemd/launchd restart
// invocation, which it fails — simulating a *real* restart failure on an
// installed service, as opposed to service.ErrNotInstalled's "no service at
// all" case (fakeServiceRunner plus no pre-install covers that one). It
// backs TestRunPairInviteRestartFailurePrintsCodeThenErrors, the P0
// regression test for the fix that keeps the invite code printing even
// when --restart itself fails.
type failingRestartServiceRunner struct{}

func (failingRestartServiceRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	for _, arg := range args {
		if arg == "restart" {
			return nil, errors.New("simulated restart failure")
		}
	}
	return nil, nil
}

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
	if !strings.Contains(output, "--member <their-network-id>:<their-agent-id>") {
		t.Fatalf("expected friend-side placeholder ids in output %q", output)
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

	if !strings.Contains(joinOutput, "moltnet admin room members add --room chat --member alice-net:<their-agent-id>") {
		t.Fatalf("expected a membership command naming alice-net in output %q", joinOutput)
	}
}

// TestRunPairJoinRestartWithoutManagedServiceWarnsInsteadOfErroring is the
// P1 regression test: a joiner running `pair <code> --restart` against a
// network with no managed service installed (a hand-run server, never
// `moltnet service install`ed) must not lose their entire aftercare over a
// failed convenience flag. The pairing is already written to disk by the
// time --restart is even attempted, so a missing service alone is now a
// warning plus the ordinary manual-restart reminder, not a command error:
// the command still exits 0, and every remaining aftercare line —
// including the membership-command guidance — still prints in full.
func TestRunPairJoinRestartWithoutManagedServiceWarnsInsteadOfErroring(t *testing.T) {
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
			"--room", "chat",
			"--config", alicePath,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	bobDir := t.TempDir()
	bobPath := writeMoltnetConfig(t, bobDir, "bob-net", "Bob's Moltnet")

	var runErr error
	joinOutput := captureStdout(t, func() {
		runErr = run(context.Background(), []string{"pair", code, "--config", bobPath, "--restart"}, "test")
	})
	if runErr != nil {
		t.Fatalf("run() pair <code> --restart error = %v, want no error: a written pairing plus a failed convenience flag should not exit nonzero", runErr)
	}
	if !strings.Contains(joinOutput, "no managed service installed for bob-net") {
		t.Fatalf("expected the missing-service warning in output %q", joinOutput)
	}
	if !strings.Contains(joinOutput, "restart the Moltnet server for this pairing to take effect") {
		t.Fatalf("expected the manual-restart reminder alongside the warning in output %q", joinOutput)
	}
	if !strings.Contains(joinOutput, "You're paired with alice-net") {
		t.Fatalf("expected the rest of aftercare to still print in full, got %q", joinOutput)
	}
	if !strings.Contains(joinOutput, "admin room members add") {
		t.Fatalf("expected the membership-command guidance to still print, got %q", joinOutput)
	}

	bobConfig, err := app.LoadConfigForPath(bobPath, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(bobConfig.Pairings) != 1 {
		t.Fatalf("expected the pairing to be persisted despite the restart warning, got %#v", bobConfig.Pairings)
	}
}

// TestRunPairInviteRestartFailurePrintsCodeThenErrors is the P0 regression
// test for a real restart failure (as opposed to the no-managed-service
// case covered by TestRunPairJoinRestartWithoutManagedServiceWarnsInsteadOfErroring):
// the network *does* have an installed service, but the restart command
// itself fails (failingRestartServiceRunner). The invite code must still
// print — the pairing was already written to disk before the restart was
// ever attempted — and the restart failure must still surface as the
// command's exit error, but only after the rest of aftercare, including the
// code, has already printed.
func TestRunPairInviteRestartFailurePrintsCodeThenErrors(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	home := t.TempDir()
	t.Setenv("HOME", home)

	previous := newServiceManager
	newServiceManager = func() *service.Manager {
		return service.NewForOS(failingRestartServiceRunner{}, "linux")
	}
	t.Cleanup(func() { newServiceManager = previous })

	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	manager := service.NewForOS(failingRestartServiceRunner{}, "linux")
	spec := service.Spec{
		NetworkID:  "alice-net",
		ConfigPath: path,
		BinaryPath: filepath.Join(directory, "moltnet"),
		NetworkDir: directory,
	}
	if err := manager.Install(context.Background(), spec); err != nil {
		t.Fatalf("pre-install fake service: %v", err)
	}

	var runErr error
	output := captureStdout(t, func() {
		runErr = run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--config", path,
			"--restart",
		}, "test")
	})

	if runErr == nil {
		t.Fatal("expected the restart failure to propagate as the command's exit error")
	}
	if !strings.Contains(runErr.Error(), "could not restart the moltnet service") {
		t.Fatalf("unexpected error %v", runErr)
	}

	code := extractInviteCode(t, output)
	if _, err := protocol.DecodeInvite(code); err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}
	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected the restart failure to also print inline as a warning, got %q", output)
	}
	if !strings.Contains(output, "Then:") {
		t.Fatalf("expected the rest of aftercare (Then: sequence) to still print, got %q", output)
	}

	config, err := app.LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Pairings) != 1 || config.Pairings[0].ID != "friend-net" {
		t.Fatalf("expected the pairing to be persisted despite the restart failure, got %#v", config.Pairings)
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
	if !strings.Contains(output, "restarted the service for bob-net") {
		t.Fatalf("expected restart confirmation in output %q", output)
	}
}

// TestRunPairInviteRestartPrintsCodeAndRestartConfirmation is the P0
// regression test: `pair invite --restart` must still print the
// moltnet-invite: code — the entire point of the command — as its own,
// unmissable paragraph (a blank line on both sides, nothing else sharing
// its line), after the restart confirmation. The bug report this guards
// against was a field paste where --restart appeared to have swallowed the
// code; investigation found the code was in fact always printed
// unconditionally as part of the aftercare block (runPairInvite always
// calls printPairInviteAftercare, which never skips it) — the real defect
// was that the old output ran the "wrote pairing" line, the bare code, and
// the restart confirmation together with no blank lines between them, so a
// long unbroken base64 code was easy to mistake for line noise. This pins
// the fixed, unmissable presentation directly rather than trusting it holds
// by construction.
func TestRunPairInviteRestartPrintsCodeAndRestartConfirmation(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	home := t.TempDir()
	t.Setenv("HOME", home)
	withFakeServiceManager(t, "linux")

	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	manager := service.NewForOS(fakeServiceRunner{}, "linux")
	spec := service.Spec{
		NetworkID:  "alice-net",
		ConfigPath: path,
		BinaryPath: filepath.Join(directory, "moltnet"),
		NetworkDir: directory,
	}
	if err := manager.Install(context.Background(), spec); err != nil {
		t.Fatalf("pre-install fake service: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--room", "chat",
			"--config", path,
			"--restart",
		}, "test"); err != nil {
			t.Fatalf("run() pair invite --restart error = %v", err)
		}
	})

	code := extractInviteCode(t, output)
	if _, err := protocol.DecodeInvite(code); err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}

	if !strings.Contains(output, "restarted the service for alice-net") {
		t.Fatalf("expected restart confirmation in output %q", output)
	}

	// The code must stand alone on its own line, with a blank line
	// immediately before and after it — never run together with the
	// status lines or the "Then:" guidance, which is what made it easy to
	// miss in the field report this test guards against.
	codeLine := "    " + code
	lines := strings.Split(output, "\n")
	found := false
	for i, line := range lines {
		if line != codeLine {
			continue
		}
		found = true
		if i == 0 || lines[i-1] != "" {
			t.Fatalf("invite code line has no blank line before it: %q", output)
		}
		if i+1 >= len(lines) || lines[i+1] != "" {
			t.Fatalf("invite code line has no blank line after it: %q", output)
		}
	}
	if !found {
		t.Fatalf("invite code not found as its own indented line in output: %q", output)
	}
}
