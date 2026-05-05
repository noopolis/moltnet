package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireUpdateLockCreatesPrivateExclusiveLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moltnet.update.lock")
	lock, err := acquireUpdateLock(updateLockOptions{Path: path})
	if err != nil {
		t.Fatalf("acquire update lock: %v", err)
	}
	defer lock.Release()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("lock permissions = %o, want 0600", got)
	}

	_, err = acquireUpdateLock(updateLockOptions{Path: path, StaleAfter: time.Hour})
	if err == nil {
		t.Fatal("expected second lock acquisition to fail")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("unexpected lock error %v", err)
	}
}

func TestAcquireUpdateLockReplacesStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moltnet.update.lock")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("age stale lock: %v", err)
	}

	lock, err := acquireUpdateLock(updateLockOptions{Path: path, StaleAfter: time.Minute})
	if err != nil {
		t.Fatalf("acquire stale lock: %v", err)
	}
	defer lock.Release()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if strings.Contains(string(contents), "old") {
		t.Fatalf("stale lock was not replaced: %q", string(contents))
	}
}

func TestAcquireUpdateLockDoesNotReplaceActiveOldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moltnet.update.lock")
	record := updateLockRecord{
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC(),
		PID:       os.Getpid(),
		Token:     "active-token",
	}
	contents, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal active lock: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write active lock: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("age active lock: %v", err)
	}

	_, err = acquireUpdateLock(updateLockOptions{Path: path, StaleAfter: time.Minute})
	if err == nil {
		t.Fatal("expected active old lock to be preserved")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("unexpected active lock error %v", err)
	}
	got, ok := readUpdateLockRecord(path)
	if !ok || got.Token != "active-token" {
		t.Fatalf("active lock was replaced: %#v ok=%v", got, ok)
	}
}

func TestUpdateLockReleaseDoesNotRemoveNewerLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moltnet.update.lock")
	lock, err := acquireUpdateLock(updateLockOptions{Path: path})
	if err != nil {
		t.Fatalf("acquire update lock: %v", err)
	}
	newer := updateLockRecord{
		CreatedAt: time.Now().UTC(),
		PID:       0,
		Token:     "new-owner",
	}
	contents, err := json.Marshal(newer)
	if err != nil {
		t.Fatalf("marshal newer lock: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("replace lock: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("release old lock: %v", err)
	}
	got, ok := readUpdateLockRecord(path)
	if !ok || got.Token != "new-owner" {
		t.Fatalf("newer lock was removed or changed: %#v ok=%v", got, ok)
	}
}

func TestAcquireUpdateLockRefusesSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	path := filepath.Join(directory, "moltnet.update.lock")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := acquireUpdateLock(updateLockOptions{Path: path})
	if err == nil {
		t.Fatal("expected symlink lock refusal")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("unexpected symlink refusal error %v", err)
	}
}
