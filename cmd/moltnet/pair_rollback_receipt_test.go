package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/app"
)

// TestWritePairingWithRollbackWritesReceiptBeforeCommit is Unit 4's core
// ordering guarantee, asserted directly: the receipt must already be on disk
// at the moment WritePairing runs, not merely by the time
// writePairingWithRollback returns. It overrides writePairingFunc (pair.go's
// test seam) to observe filesystem state from inside the call that would
// otherwise be an opaque app.WritePairing invocation -- a mutation that
// reordered the two calls (receipt write moved after the reload check)
// would still leave every other test in this package green, but would flip
// receiptExistedDuringWrite to false here.
func TestWritePairingWithRollbackWritesReceiptBeforeCommit(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	receipt := app.InviteReceipt{
		PairingID: "friend-net",
		Code:      "moltnet-invite:ordering-check-code",
		Exp:       time.Now().Add(time.Hour).UTC(),
	}

	original := writePairingFunc
	t.Cleanup(func() { writePairingFunc = original })

	var receiptExistedDuringWrite bool
	writePairingFunc = func(p string, pairing app.PairingWriteback, authToken app.AuthTokenWriteback, roomIDs []string, force bool) error {
		if _, ok, err := app.LoadInviteReceipt(p, receipt.PairingID); err == nil && ok {
			receiptExistedDuringWrite = true
		}
		return original(p, pairing, authToken, roomIDs, force)
	}

	pairing := app.PairingWriteback{
		ID:    "friend-net",
		Token: "pairing-token-value",
		Relay: &app.PairingRelayWriteback{URL: "wss://relay.example.dev", Room: "room-1", Token: "relay-token-value"},
	}
	authToken := app.AuthTokenWriteback{ID: "friend-net", Value: "pairing-token-value", Scopes: []string{"pair"}}

	if err := writePairingWithRollback(path, pairing, authToken, nil, false, &receipt); err != nil {
		t.Fatalf("writePairingWithRollback() error = %v", err)
	}
	if !receiptExistedDuringWrite {
		t.Fatal("invite receipt was not present on disk at the moment WritePairing ran")
	}
}

// TestWritePairingWithRollbackRestoresPriorReceiptOnWritePairingFailure is
// Unit 4's symmetric snapshot/restore guarantee, half one: when WritePairing
// itself fails (here, ErrPairingExists without --force -- a failure that
// happens before any file is touched), a *prior* receipt for the same
// pairing id must survive untouched, byte-identical, not be clobbered by the
// new one that never actually committed. Neutering the receiptSnapshot.restore
// call on this failure branch (pair.go) would leave the new, never-valid
// code in place instead.
func TestWritePairingWithRollbackRestoresPriorReceiptOnWritePairingFailure(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, app.DefaultPath)
	contents := `version: moltnet.v1

network:
  id: alice-net
  name: Alice's Moltnet

pairings:
  - id: friend-net
    remote_network_id: bob-net
    token: existing-pairing-token
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	priorReceipt := app.InviteReceipt{
		PairingID: "friend-net",
		Code:      "moltnet-invite:prior-code-must-survive",
		Exp:       time.Now().Add(time.Hour).UTC(),
	}
	if err := app.WriteInviteReceipt(path, priorReceipt); err != nil {
		t.Fatalf("WriteInviteReceipt() error = %v", err)
	}

	newPairing := app.PairingWriteback{
		ID:    "friend-net",
		Token: "new-pairing-token",
		Relay: &app.PairingRelayWriteback{URL: "wss://relay.example.dev", Room: "room-2", Token: "relay-token-2"},
	}
	newAuthToken := app.AuthTokenWriteback{ID: "friend-net", Value: "new-pairing-token", Scopes: []string{"pair"}}
	newReceipt := app.InviteReceipt{
		PairingID: "friend-net",
		Code:      "moltnet-invite:new-code-should-never-persist",
		Exp:       time.Now().Add(2 * time.Hour).UTC(),
	}

	err := writePairingWithRollback(path, newPairing, newAuthToken, nil, false, &newReceipt)
	if err == nil || !errors.Is(err, app.ErrPairingExists) {
		t.Fatalf("writePairingWithRollback() error = %v, want ErrPairingExists", err)
	}

	restored, ok, loadErr := app.LoadInviteReceipt(path, "friend-net")
	if loadErr != nil {
		t.Fatalf("LoadInviteReceipt() error = %v", loadErr)
	}
	if !ok {
		t.Fatal("prior receipt missing after WritePairing failure, want it restored")
	}
	if restored.Code != priorReceipt.Code {
		t.Fatalf("restored receipt code = %q, want byte-identical prior code %q", restored.Code, priorReceipt.Code)
	}
}

// TestWritePairingWithRollbackRemovesFreshReceiptOnReloadFailure is Unit 4's
// symmetric snapshot/restore guarantee, half two: when WritePairing succeeds
// but the post-write reload check fails (the same fixture
// TestWritePairingWithRollbackRestoresConfigOnReloadFailure, pair_test.go,
// uses), a *fresh* invite's receipt -- one with no prior version to restore
// -- must be removed along with the rolled-back config, not left behind
// describing a pairing that no longer exists anywhere. Neutering the
// receiptSnapshot.restore call on this branch (pair.go) would leave it on
// disk.
func TestWritePairingWithRollbackRemovesFreshReceiptOnReloadFailure(t *testing.T) {
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	// Neither remote_base_url nor relay is set: WritePairing's own
	// structural write does not validate that constraint, but the post-write
	// LoadConfigForPath reload does, so this fails the reload check, not
	// WritePairing itself.
	pairing := app.PairingWriteback{ID: "friend-net", RemoteNetworkID: "bob-net", Token: "pairing-token"}
	authToken := app.AuthTokenWriteback{ID: "friend-net", Value: "pairing-token", Scopes: []string{"pair"}}
	receipt := app.InviteReceipt{
		PairingID: "friend-net",
		Code:      "moltnet-invite:fresh-code-must-not-survive",
		Exp:       time.Now().Add(time.Hour).UTC(),
	}

	err := writePairingWithRollback(path, pairing, authToken, nil, false, &receipt)
	if err == nil {
		t.Fatal("expected writePairingWithRollback to fail the post-write reload")
	}

	if _, ok, loadErr := app.LoadInviteReceipt(path, "friend-net"); loadErr != nil {
		t.Fatalf("LoadInviteReceipt() after reload failure error = %v", loadErr)
	} else if ok {
		t.Fatal("fresh invite's receipt still present after a rolled-back reload failure")
	}
}
