package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// buildSetupTestInviteCode encodes a standalone invite code for the setup
// wizard's "I have an invite code" tests, independent of any `pair invite`
// call — these tests exercise Q7's own decode-and-validate step, not the
// invite-generation side.
func buildSetupTestInviteCode(t *testing.T, exp time.Time) string {
	t.Helper()
	code, err := protocol.EncodeInvite(protocol.Invite{
		RelayURL:     "wss://moltnet-relay.acme.workers.dev",
		Room:         "friend-room-token",
		RelayToken:   "friend-relay-token",
		PairingToken: "friend-pairing-token",
		PairingID:    "friend-net",
		NetworkID:    "acme-net",
		NetworkName:  "Acme Net",
		SharedRooms:  []string{"general"},
		Exp:          exp,
	})
	if err != nil {
		t.Fatalf("EncodeInvite() error = %v", err)
	}
	return code
}

// TestSetupQ7HaveCodeValidPairs covers Q7 = "I have an invite code" end to
// end: a valid code pairs, and the resulting config carries the pairing.
func TestSetupQ7HaveCodeValidPairs(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	code := buildSetupTestInviteCode(t, time.Now().Add(48*time.Hour))

	// Q1, Q2, Q4, Q5, Q6="2" (no service), Q7="3" (I have a code), then the
	// pasted code.
	withPromptAnswers(t, "", "", "", "", "2", "3", code)

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})

	if !strings.Contains(output, "paired with acme-net") {
		t.Fatalf("expected the completion screen to name the remote network, got %q", output)
	}

	cfg, err := app.LoadFile(writtenSetupConfigPath(home), "")
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if len(cfg.Pairings) != 1 || cfg.Pairings[0].ID != "friend-net" {
		t.Fatalf("cfg.Pairings = %+v, want exactly one pairing named %q", cfg.Pairings, "friend-net")
	}
	if cfg.Pairings[0].RemoteNetworkID != "acme-net" {
		t.Fatalf("cfg.Pairings[0].RemoteNetworkID = %q, want %q", cfg.Pairings[0].RemoteNetworkID, "acme-net")
	}
}

// TestSetupQ7HaveCodeCollidingNetworkIDRefusesBeforeMutation is P2-1's
// regression test: `pair` itself refuses an invite whose NetworkID collides
// with the local network id (pair_join.go), but only after `init` has
// already written the config to disk — planSetupPairHaveCode must catch the
// exact same collision itself, before init (or any other mutation) ever
// runs. The invite's NetworkID here is "local" — the same default Q2 itself
// falls back to on an empty answer — so this drives the collision through
// the Enter-through path rather than a hand-picked id.
func TestSetupQ7HaveCodeCollidingNetworkIDRefusesBeforeMutation(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	code, err := protocol.EncodeInvite(protocol.Invite{
		RelayURL:     "wss://moltnet-relay.acme.workers.dev",
		Room:         "friend-room-token",
		RelayToken:   "friend-relay-token",
		PairingToken: "friend-pairing-token",
		PairingID:    "friend-net",
		NetworkID:    app.DefaultNetworkID,
		NetworkName:  "Local",
		SharedRooms:  []string{"general"},
		Exp:          time.Now().Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("EncodeInvite() error = %v", err)
	}

	// Q1, Q2="" (default network id, matching the invite's own NetworkID),
	// Q4, Q5, Q6="2" (no service), Q7="3" (I have a code), then the pasted
	// code.
	withPromptAnswers(t, "", "", "", "", "2", "3", code)

	var runErr error
	output := captureSetupOutput(t, func() {
		runErr = run(context.Background(), []string{"setup"}, "test")
	})
	if runErr == nil {
		t.Fatal("expected the network-id collision refusal, got a successful run")
	}
	if !errors.Is(runErr, errSetupPairNetworkIDCollision) {
		t.Fatalf("run() error = %v, want it to wrap errSetupPairNetworkIDCollision", runErr)
	}
	if strings.Contains(output, "network initialized") {
		t.Fatalf("expected the refusal to land before `init` ever ran, got %q", output)
	}
	if _, err := app.LoadFile(writtenSetupConfigPath(home), ""); err == nil {
		t.Fatal("expected no config to have been written before the refusal")
	}
}

// TestSetupQ7HaveCodeExpiredProducesClearMessage covers "an expired [code]
// produces a clear message" rather than a raw transport error: the wizard
// must decode and validate the code itself, before ever invoking `pair`, so
// this must fail with no pairing ever written.
func TestSetupQ7HaveCodeExpiredProducesClearMessage(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	code := buildSetupTestInviteCode(t, time.Now().Add(-1*time.Hour))

	withPromptAnswers(t, "", "", "", "", "2", "3", code)

	var runErr error
	captureSetupOutput(t, func() {
		runErr = run(context.Background(), []string{"setup"}, "test")
	})
	if runErr == nil {
		t.Fatal("expected an error for an expired invite code")
	}
	if !strings.Contains(runErr.Error(), "expired") {
		t.Fatalf("run() error = %v, want a clear \"expired\" message rather than a raw transport error", runErr)
	}

	// The code is decoded and validated before planSetupPairing ever
	// returns, which is before runSetupInteractive builds or executes any
	// invocation at all (setup.go) — so not even `init` has run yet, and no
	// config exists on disk to load.
	if _, err := app.LoadFile(writtenSetupConfigPath(home), ""); err == nil {
		t.Fatal("expected no config to have been written before the code was validated")
	}
}

// TestSetupQ7HaveCodeMalformedProducesClearMessage is the malformed-code
// counterpart: garbage input must not reach `pair` as a raw positional
// argument and surface whatever protocol.DecodeInvite happens to produce
// deep in that call path.
func TestSetupQ7HaveCodeMalformedProducesClearMessage(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	withPromptAnswers(t, "", "", "", "", "2", "3", "not-a-real-invite-code")

	var runErr error
	captureSetupOutput(t, func() {
		runErr = run(context.Background(), []string{"setup"}, "test")
	})
	if runErr == nil {
		t.Fatal("expected an error for a malformed invite code")
	}
	if !strings.Contains(runErr.Error(), "expired or malformed") {
		t.Fatalf("run() error = %v, want the wizard's own clear malformed-code message", runErr)
	}
}
