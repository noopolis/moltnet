package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestPairInviteReceiptRecoversAfterInterruptionBeforeDisplay is Unit 4's
// core regression: before app.InviteReceipt existed, `pair invite` committed
// the pairing to config and only then printed the code
// (printPairInviteAftercare) -- a crash between those two steps lost the
// code irrecoverably even though the pairing it belongs to was already live.
//
// writePairingWithRollback (pair.go) is exactly the call site that commits
// the pairing; printPairInviteAftercare is the call site that prints it.
// This test calls the former and never calls the latter at all, which *is*
// "interrupted between the commit and the display" -- then recovers through
// the same app.LoadInviteReceipt a real `pair invite show` would use, and
// checks the recovered code is the exact bytes protocol.EncodeInvite
// produced, not a re-derived approximation.
func TestPairInviteReceiptRecoversAfterInterruptionBeforeDisplay(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	invite := protocol.Invite{
		RelayURL:     "wss://relay.example.dev",
		Room:         "relay-room",
		RelayToken:   "relay-token-value",
		PairingToken: "pairing-token-value",
		PairingID:    "friend-net",
		NetworkID:    "alice-net",
		NetworkName:  "Alice's Moltnet",
		Exp:          time.Now().UTC().Add(protocol.DefaultInviteTTL),
	}
	code, err := protocol.EncodeInvite(invite)
	if err != nil {
		t.Fatalf("EncodeInvite() error = %v", err)
	}

	pairing := app.PairingWriteback{
		ID:    invite.PairingID,
		Token: invite.PairingToken,
		Relay: &app.PairingRelayWriteback{URL: invite.RelayURL, Room: invite.Room, Token: invite.RelayToken},
	}
	authToken := app.AuthTokenWriteback{ID: invite.PairingID, Value: invite.PairingToken, Scopes: []string{"pair"}}
	receipt := app.InviteReceipt{PairingID: invite.PairingID, Code: code, Exp: invite.Exp}

	if err := writePairingWithRollback(path, pairing, authToken, nil, false, &receipt); err != nil {
		t.Fatalf("writePairingWithRollback() error = %v", err)
	}

	// Simulated crash: no printPairInviteAftercare call happens between the
	// line above and the recovery below.

	recovered, ok, err := app.LoadInviteReceipt(path, invite.PairingID)
	if err != nil {
		t.Fatalf("LoadInviteReceipt() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadInviteReceipt() ok = false after a committed invite, want true")
	}
	if recovered.Code != code {
		t.Fatalf("recovered code = %q, want byte-identical %q", recovered.Code, code)
	}
}

// TestRunPairInviteShowRecoversByteIdenticalCode exercises the same recovery
// path end to end through the actual CLI surface: a normal `pair invite`
// run, then `pair invite show <id>` in a separate process-equivalent
// invocation (no shared in-memory state), asserting the recovered code
// matches the originally printed one exactly.
func TestRunPairInviteShowRecoversByteIdenticalCode(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	showOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite", "show", "friend-net",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite show error = %v", err)
		}
	})

	recovered := strings.TrimSpace(showOutput)
	if recovered != code {
		t.Fatalf("pair invite show output = %q, want byte-identical %q", recovered, code)
	}
}

// TestRunPairInviteWritesReceiptWithSecureMode is the CLI-level companion to
// internal/app's TestWriteInviteReceiptModeIs0600: the receipt a real `pair
// invite` invocation leaves on disk must never be world- or group-readable,
// since it is exactly as sensitive as the pairing token beside it.
func TestRunPairInviteWritesReceiptWithSecureMode(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})

	info, err := os.Stat(app.InviteReceiptPath(path, "friend-net"))
	if err != nil {
		t.Fatalf("stat invite receipt: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("invite receipt mode = %v, want 0600", info.Mode().Perm())
	}
}

// TestRunPairInviteShowRejectsExpiredInvite is the "expiry is preserved and
// surfaced" requirement: a receipt whose invite has already expired must be
// reported as expired, never handed back as a usable code.
//
// The pairing is written to config first (via app.WritePairing directly,
// bypassing the CLI) so this test exercises the expiry check specifically,
// independent of Fix 2's "pairing must still be configured" guard
// (runPairInviteShow/pairingConfigured, pair_invite.go) -- that guard is
// covered separately by TestRunPairInviteShowRefusesWhenPairingNotConfigured.
func TestRunPairInviteShowRejectsExpiredInvite(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	pairing := app.PairingWriteback{
		ID:    "friend-net",
		Token: "pairing-token-value",
		Relay: &app.PairingRelayWriteback{URL: "wss://relay.example.dev", Room: "room-1", Token: "relay-token-value"},
	}
	authToken := app.AuthTokenWriteback{ID: "friend-net", Value: "pairing-token-value", Scopes: []string{"pair"}}
	if err := app.WritePairing(path, pairing, authToken, nil, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	expired := app.InviteReceipt{
		PairingID: "friend-net",
		Code:      "moltnet-invite:stale-code-should-never-be-printed",
		Exp:       time.Now().Add(-time.Hour).UTC(),
	}
	if err := app.WriteInviteReceipt(path, expired); err != nil {
		t.Fatalf("WriteInviteReceipt() error = %v", err)
	}

	var runErr error
	output := captureStdout(t, func() {
		runErr = run(context.Background(), []string{
			"pair", "invite", "show", "friend-net",
			"--config", path,
		}, "test")
	})
	if runErr == nil || !strings.Contains(runErr.Error(), "expired") {
		t.Fatalf("run() pair invite show error = %v, want expiry refusal", runErr)
	}
	if strings.Contains(output, expired.Code) {
		t.Fatalf("expired invite code leaked to stdout: %q", output)
	}
}

// TestRunPairInviteShowReportsMissingReceipt covers a pairing id with no
// receipt at all -- never generated with a receipt (a --print-only invite,
// or one created before this feature existed) or already revoked -- which
// must say so rather than failing some other, more confusing way.
func TestRunPairInviteShowReportsMissingReceipt(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	err := run(context.Background(), []string{
		"pair", "invite", "show", "never-generated",
		"--config", path,
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "no recoverable invite") {
		t.Fatalf("run() pair invite show error = %v, want a clear no-receipt message", err)
	}
}

// TestRunPairRevokeRemovesInviteReceipt is the "revocation removes it"
// requirement: `pair revoke` already strips the pairings[] entry and the
// peer's auth.tokens[] credential (app.RevokePairing); it must strip the
// invite receipt too, or a revoked pairing leaves a live-looking invite
// code recoverable via `pair invite show` on disk.
func TestRunPairRevokeRemovesInviteReceipt(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})

	if _, ok, err := app.LoadInviteReceipt(path, "friend-net"); err != nil || !ok {
		t.Fatalf("expected an invite receipt before revoke, ok=%v err=%v", ok, err)
	}

	if _, err := revokePairingWithRollback(path, "friend-net"); err != nil {
		t.Fatalf("revokePairingWithRollback() error = %v", err)
	}

	if _, ok, err := app.LoadInviteReceipt(path, "friend-net"); err != nil {
		t.Fatalf("LoadInviteReceipt() after revoke error = %v", err)
	} else if ok {
		t.Fatal("invite receipt still present after pair revoke")
	}
}

// TestRunPairInviteShowRefusesForRevokedPairingWithOrphanedReceipt is F2's
// first live-confirmed state: a revoke commits (the pairings[] entry and its
// auth token are gone) but RemoveInviteReceipt fails -- reproduced here the
// same way the reviewer did, with a read-only invites directory -- so the
// receipt survives on disk after an otherwise "successful" revoke. Before
// this fix, `pair invite show` trusted the receipt alone and handed back the
// full code, relay URL, and relay token with exit 0, even though the config
// has zero pairings.
func TestRunPairInviteShowRefusesForRevokedPairingWithOrphanedReceipt(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--config", path,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	invitesDir := filepath.Dir(app.InviteReceiptPath(path, "friend-net"))
	if err := os.Chmod(invitesDir, 0o500); err != nil {
		t.Fatalf("chmod invites dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(invitesDir, 0o700) })

	// RevokePairing itself never touches .moltnet/invites, so the config
	// commit succeeds; only the RemoveInviteReceipt step below it fails
	// against the now-read-only directory -- the reviewer's exact repro.
	var revokeErr error
	captureStdout(t, func() {
		revokeErr = run(context.Background(), []string{"pair", "revoke", "friend-net", "--config", path}, "test")
	})
	if revokeErr == nil {
		t.Fatal("expected pair revoke to surface the RemoveInviteReceipt failure")
	}

	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Pairings) != 0 {
		t.Fatalf("expected zero pairings after revoke, got %#v", config.Pairings)
	}

	if _, ok, err := app.LoadInviteReceipt(path, "friend-net"); err != nil || !ok {
		t.Fatalf("expected the orphaned receipt to still be present, ok=%v err=%v", ok, err)
	}

	if err := os.Chmod(invitesDir, 0o700); err != nil {
		t.Fatalf("restore invites dir permissions: %v", err)
	}

	var showErr error
	output := captureStdout(t, func() {
		showErr = run(context.Background(), []string{
			"pair", "invite", "show", "friend-net",
			"--config", path,
		}, "test")
	})
	if showErr == nil {
		t.Fatal("expected pair invite show to refuse a receipt for a pairing no longer present in config")
	}
	if strings.Contains(output, code) {
		t.Fatalf("revoked pairing's invite code leaked to stdout: %q", output)
	}
}

// TestRunPairInviteShowRefusesOrphanReceiptFromInterruptedInvite is F2's
// second live-confirmed state: an interruption between the receipt commit
// and the pairing commit (writePairingWithRollback, pair.go) leaves a
// receipt for a pairing that was never actually written to config --
// simulated here the same way TestPairInviteReceiptRecoversAfterInterruptionBeforeDisplay
// simulates the opposite (successful) case, by writing only the receipt and
// never calling WritePairing at all. Before this fix, `pair invite show`
// handed the code back anyway, and `pair revoke` could never clear it --
// RevokePairing fails "pairing not found" before ever reaching
// RemoveInviteReceipt, so no CLI path could ever clear the orphan. Both
// halves of the fix are exercised here: show now refuses it, and revoke --
// despite still correctly failing "pairing not found" -- now clears the
// receipt as a side effect, so a second show call reports it gone entirely
// instead of looping back to the same orphan forever.
func TestRunPairInviteShowRefusesOrphanReceiptFromInterruptedInvite(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	ghost := app.InviteReceipt{
		PairingID: "ghost",
		Code:      "moltnet-invite:orphan-code-should-never-be-printed",
		Exp:       time.Now().Add(time.Hour).UTC(),
	}
	if err := app.WriteInviteReceipt(path, ghost); err != nil {
		t.Fatalf("WriteInviteReceipt() error = %v", err)
	}

	var showErr error
	output := captureStdout(t, func() {
		showErr = run(context.Background(), []string{
			"pair", "invite", "show", "ghost",
			"--config", path,
		}, "test")
	})
	if showErr == nil {
		t.Fatal("expected pair invite show to refuse an orphan receipt for a pairing never committed to config")
	}
	if strings.Contains(output, ghost.Code) {
		t.Fatalf("orphan invite code leaked to stdout: %q", output)
	}

	var revokeErr error
	captureStdout(t, func() {
		revokeErr = run(context.Background(), []string{"pair", "revoke", "ghost", "--config", path}, "test")
	})
	if revokeErr == nil || !strings.Contains(revokeErr.Error(), "not found") {
		t.Fatalf("run() pair revoke ghost error = %v, want a not-found error (nothing to revoke)", revokeErr)
	}

	if _, ok, err := app.LoadInviteReceipt(path, "ghost"); err != nil {
		t.Fatalf("LoadInviteReceipt() after revoke error = %v", err)
	} else if ok {
		t.Fatal("orphan receipt still present after pair revoke; no CLI path could ever clear it")
	}
}
