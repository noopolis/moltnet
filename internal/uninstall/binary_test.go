package uninstall

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveBinaryDeletesACopiedBinary is the self-deletion test PLAN.md
// phase 5 calls for: it never touches the real running go test binary
// (deleting that mid-run would be actively dangerous), only a throwaway
// copy in a temp dir standing in for "the moltnet executable at
// os.Executable()".
func TestRemoveBinaryDeletesACopiedBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moltnet")
	writeExecutable(t, path)

	if err := RemoveBinary(path); err != nil {
		t.Fatalf("RemoveBinary() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, stat err = %v", path, err)
	}
}

func TestRemoveBinaryEmptyPathErrors(t *testing.T) {
	if err := RemoveBinary(""); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}

func TestRemoveBinaryPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permission checks")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "moltnet")
	writeExecutable(t, path)

	// Removing a file requires write permission on its parent directory,
	// not the file itself, so lock down dir to read+execute only —
	// simulating a root-owned install directory such as /usr/local/bin.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod %q: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := RemoveBinary(path)
	if err == nil {
		t.Fatal("expected a permission-denied error")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("RemoveBinary() error = %v, want errors.Is(err, fs.ErrPermission)", err)
	}
}
