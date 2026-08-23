package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestInviteReceipt() InviteReceipt {
	return InviteReceipt{
		PairingID: "friend-net",
		Code:      "moltnet-invite:test-code-payload",
		Exp:       time.Now().Add(24 * time.Hour).UTC(),
	}
}

func TestWriteInviteReceiptRoundTrips(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	receipt := newTestInviteReceipt()
	if err := WriteInviteReceipt(path, receipt); err != nil {
		t.Fatalf("WriteInviteReceipt() error = %v", err)
	}

	loaded, ok, err := LoadInviteReceipt(path, receipt.PairingID)
	if err != nil {
		t.Fatalf("LoadInviteReceipt() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadInviteReceipt() ok = false, want true")
	}
	if loaded.Code != receipt.Code {
		t.Fatalf("loaded code = %q, want byte-identical %q", loaded.Code, receipt.Code)
	}
	if !loaded.Exp.Equal(receipt.Exp) {
		t.Fatalf("loaded exp = %v, want %v", loaded.Exp, receipt.Exp)
	}
}

func TestLoadInviteReceiptMissingIsNotAnError(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	_, ok, err := LoadInviteReceipt(path, "never-generated")
	if err != nil {
		t.Fatalf("LoadInviteReceipt() error = %v, want nil for a receipt that was never written", err)
	}
	if ok {
		t.Fatal("LoadInviteReceipt() ok = true, want false for a receipt that was never written")
	}
}

// TestLoadInviteReceiptRejectsEmptyCodeAsCorrupt pins the fix for a real
// gap: a `null` or `{}` receipt body used to decode to a zero-value
// InviteReceipt with ok=true, so `pair invite show` returned err=nil and
// printed an empty line -- a wizard scraping stdout got "" back as "the
// code," indistinguishable from success. It must now be reported as a
// corrupt receipt.
func TestLoadInviteReceiptRejectsEmptyCodeAsCorrupt(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	receiptPath := InviteReceiptPath(path, "friend-net")
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatalf("mkdir invites dir: %v", err)
	}
	if err := os.WriteFile(receiptPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write corrupt receipt: %v", err)
	}

	_, ok, err := LoadInviteReceipt(path, "friend-net")
	if err == nil {
		t.Fatal("LoadInviteReceipt() error = nil, want an error for a corrupt (empty-code) receipt")
	}
	if ok {
		t.Fatal("LoadInviteReceipt() ok = true, want false for a corrupt receipt")
	}
}

// TestWriteInviteReceiptTightensPreExistingWideDirectoryPermissions pins the
// fix for a real gap: MkdirAll no-ops on a directory that already exists, so
// a pre-existing wider-than-0700 ".moltnet/invites" (e.g. left behind by an
// old version, or a hand-created directory) stayed that way after a
// successful write -- the 0600 receipt file itself is unreadable by another
// user, but freely unlinkable and replaceable by one, since a directory's
// own write/execute bits (not its entries' modes) govern renaming or
// removing a name inside it.
func TestWriteInviteReceiptTightensPreExistingWideDirectoryPermissions(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	invitesDir := filepath.Join(directory, ".moltnet", "invites")
	if err := os.MkdirAll(invitesDir, 0o777); err != nil {
		t.Fatalf("mkdir pre-existing wide invites dir: %v", err)
	}
	if err := os.Chmod(invitesDir, 0o777); err != nil {
		t.Fatalf("chmod invites dir to 0777: %v", err)
	}

	if err := WriteInviteReceipt(path, newTestInviteReceipt()); err != nil {
		t.Fatalf("WriteInviteReceipt() error = %v", err)
	}

	info, err := os.Stat(invitesDir)
	if err != nil {
		t.Fatalf("stat invites dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("invites dir mode = %v, want 0700 (tightened even though it already existed)", info.Mode().Perm())
	}
}

func TestWriteInviteReceiptModeIs0600(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	receipt := newTestInviteReceipt()
	if err := WriteInviteReceipt(path, receipt); err != nil {
		t.Fatalf("WriteInviteReceipt() error = %v", err)
	}

	info, err := os.Stat(InviteReceiptPath(path, receipt.PairingID))
	if err != nil {
		t.Fatalf("stat invite receipt: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("invite receipt mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteInviteReceiptRejectsSymlinkedTarget(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	receipt := newTestInviteReceipt()
	receiptPath := InviteReceiptPath(path, receipt.PairingID)

	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatalf("create invites directory: %v", err)
	}
	outsideTarget := filepath.Join(directory, "outside-target.json")
	if err := os.WriteFile(outsideTarget, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(outsideTarget, receiptPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if err := WriteInviteReceipt(path, receipt); err == nil {
		t.Fatal("expected symlink rejection error")
	}

	// The symlinked target itself must be left untouched -- a rejected write
	// must never follow the symlink and overwrite whatever it points at.
	contents, err := os.ReadFile(outsideTarget)
	if err != nil {
		t.Fatalf("read symlink target after rejected write: %v", err)
	}
	if string(contents) != "{}" {
		t.Fatalf("symlink target was modified despite rejection: %q", contents)
	}
}

func TestInviteReceiptExpired(t *testing.T) {
	t.Parallel()

	now := time.Now()
	future := InviteReceipt{Exp: now.Add(time.Hour)}
	past := InviteReceipt{Exp: now.Add(-time.Hour)}
	zero := InviteReceipt{}

	if future.Expired(now) {
		t.Fatal("future-expiring receipt reported as expired")
	}
	if !past.Expired(now) {
		t.Fatal("past-expiring receipt not reported as expired")
	}
	if zero.Expired(now) {
		t.Fatal("zero-value Exp reported as expired")
	}
}

func TestRemoveInviteReceiptDeletesFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	receipt := newTestInviteReceipt()
	if err := WriteInviteReceipt(path, receipt); err != nil {
		t.Fatalf("WriteInviteReceipt() error = %v", err)
	}

	if err := RemoveInviteReceipt(path, receipt.PairingID); err != nil {
		t.Fatalf("RemoveInviteReceipt() error = %v", err)
	}

	if _, err := os.Stat(InviteReceiptPath(path, receipt.PairingID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invite receipt still present after RemoveInviteReceipt(): err = %v", err)
	}

	_, ok, err := LoadInviteReceipt(path, receipt.PairingID)
	if err != nil {
		t.Fatalf("LoadInviteReceipt() after removal error = %v", err)
	}
	if ok {
		t.Fatal("LoadInviteReceipt() after removal ok = true, want false")
	}
}

func TestRemoveInviteReceiptMissingIsNotAnError(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	if err := RemoveInviteReceipt(path, "never-generated"); err != nil {
		t.Fatalf("RemoveInviteReceipt() error = %v, want nil for an already-absent receipt", err)
	}
}

// TestWriteInviteReceiptRejectsSymlinkedInvitesDirectory is Fix 3's
// regression: rejectSymlinkedPathIfExists (formerly plain os.Lstat) only
// ever checked the receipt file's own leaf, so pre-creating ".moltnet" (or
// ".moltnet/invites") as a symlink to an outside directory let
// atomicWriteSecretFile's os.MkdirAll traverse it without complaint --
// verified live: the receipt landed at the symlink's target with err=nil.
// WriteInviteReceipt now resolves every step through os.Root
// (os.OpenRoot(filepath.Dir(configPath))), which refuses to walk a
// symlinked path component that points outside that root at all.
func TestWriteInviteReceiptRejectsSymlinkedInvitesDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	outside := t.TempDir()

	if err := os.Symlink(outside, filepath.Join(directory, ".moltnet")); err != nil {
		t.Fatalf("create symlinked .moltnet: %v", err)
	}

	receipt := newTestInviteReceipt()
	if err := WriteInviteReceipt(path, receipt); err == nil {
		t.Fatal("expected WriteInviteReceipt to refuse a symlinked .moltnet directory")
	}

	// The escape must be refused, not merely reported: nothing should have
	// landed inside the outside directory the symlink points at.
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("outside directory received unexpected entries via the symlink escape: %#v", entries)
	}
}

// TestWriteInviteReceiptRejectsSymlinkedInvitesSubdirectory is the same
// finding one level deeper: ".moltnet" itself is real, but
// ".moltnet/invites" (the actual parent of the receipt file) is the symlink
// pointing outside. This must be refused identically.
func TestWriteInviteReceiptRejectsSymlinkedInvitesSubdirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	outside := t.TempDir()

	if err := os.MkdirAll(filepath.Join(directory, ".moltnet"), 0o700); err != nil {
		t.Fatalf("create .moltnet: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, ".moltnet", "invites")); err != nil {
		t.Fatalf("create symlinked .moltnet/invites: %v", err)
	}

	receipt := newTestInviteReceipt()
	if err := WriteInviteReceipt(path, receipt); err == nil {
		t.Fatal("expected WriteInviteReceipt to refuse a symlinked .moltnet/invites directory")
	}

	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("outside directory received unexpected entries via the symlink escape: %#v", entries)
	}
}

// TestInviteReceiptPathDoesNotCollideOnWhitespace is Fix 4's regression:
// InviteReceiptPath used to trim pairingID before hashing, so " friend " and
// "friend" resolved to the same file -- verified live, writing " friend "
// clobbered "friend"'s receipt, and RemoveInviteReceipt(" friend ") deleted
// "friend"'s. `pair <code>` writes an invite's pairing_id into pairings[]
// verbatim (no trim), so a peer-chosen whitespace id reaching this hash
// unnormalized was reachable from the network side, not just a local typo.
func TestInviteReceiptPathDoesNotCollideOnWhitespace(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	plain := InviteReceipt{PairingID: "friend", Code: "moltnet-invite:plain-code", Exp: time.Now().Add(time.Hour).UTC()}
	padded := InviteReceipt{PairingID: " friend ", Code: "moltnet-invite:padded-code", Exp: time.Now().Add(time.Hour).UTC()}

	if err := WriteInviteReceipt(path, plain); err != nil {
		t.Fatalf("WriteInviteReceipt(plain) error = %v", err)
	}
	if err := WriteInviteReceipt(path, padded); err != nil {
		t.Fatalf("WriteInviteReceipt(padded) error = %v", err)
	}

	if InviteReceiptPath(path, plain.PairingID) == InviteReceiptPath(path, padded.PairingID) {
		t.Fatal("InviteReceiptPath collided for \"friend\" and \" friend \"")
	}

	loadedPlain, ok, err := LoadInviteReceipt(path, "friend")
	if err != nil || !ok {
		t.Fatalf("LoadInviteReceipt(plain) ok=%v err=%v", ok, err)
	}
	if loadedPlain.Code != plain.Code {
		t.Fatalf("plain receipt code = %q, want untouched %q (clobbered by whitespace-padded id)", loadedPlain.Code, plain.Code)
	}

	// Removing the padded id must not remove the plain one.
	if err := RemoveInviteReceipt(path, " friend "); err != nil {
		t.Fatalf("RemoveInviteReceipt(padded) error = %v", err)
	}
	if _, ok, err := LoadInviteReceipt(path, "friend"); err != nil || !ok {
		t.Fatalf("plain receipt removed by RemoveInviteReceipt(\" friend \"): ok=%v err=%v", ok, err)
	}
}

// TestInviteReceiptPathIsSafeAgainstPathTraversal locks in
// InviteReceiptPath's hashing decision: a pairing id is operator/invite
// supplied and has no filename-safety grammar of its own (see
// InviteReceiptPath's doc comment), so a hostile id must never resolve
// outside the invites directory.
func TestInviteReceiptPathIsSafeAgainstPathTraversal(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")

	hostile := InviteReceiptPath(path, "../../etc/passwd")
	invitesDir := filepath.Join(directory, ".moltnet", "invites")

	rel, err := filepath.Rel(invitesDir, hostile)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if rel == ".." || filepath.IsAbs(rel) || len(rel) >= 2 && rel[:2] == ".." {
		t.Fatalf("InviteReceiptPath escaped its directory: %q", hostile)
	}
}
