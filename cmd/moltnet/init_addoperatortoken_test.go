package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
)

// TestAddOperatorTokenWithRollbackRestoresConfigOnReloadFailure covers
// addOperatorTokenWithRollback's rollback path directly: a pre-existing
// pairing that declares neither remote_base_url nor relay passes
// app.AddOperatorToken's own structural write (it only touches auth, never
// pairings), but fails the post-write app.LoadConfigForPath reload check,
// which does validate pairings. This mirrors
// TestWritePairingWithRollbackRestoresConfigOnReloadFailure in pair_test.go,
// giving the init --bearer-on-existing path the same rollback guarantee.
func TestAddOperatorTokenWithRollbackRestoresConfigOnReloadFailure(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	contents := "" +
		"version: moltnet.v1\n" +
		"network:\n" +
		"  id: alice-net\n" +
		"  name: Alice's Moltnet\n" +
		"pairings:\n" +
		"  - id: existing-pair\n" +
		"    remote_network_id: bob-net\n" +
		"    token: existing-pair-token\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config before: %v", err)
	}

	token := app.AuthTokenWriteback{
		ID:     "operator",
		Value:  "operator-token-value",
		Scopes: []string{"observe", "write", "admin", "pair"},
	}

	err = addOperatorTokenWithRollback(path, token)
	if err == nil {
		t.Fatal("expected addOperatorTokenWithRollback to fail the post-write reload")
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
