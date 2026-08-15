package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func TestOtherCopiesFindsCopiesNotTheCurrentBinary(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	dirC := t.TempDir() // no moltnet binary here

	current := filepath.Join(dirA, "moltnet")
	other := filepath.Join(dirB, "moltnet")
	writeExecutable(t, current)
	writeExecutable(t, other)

	pathEnv := dirA + string(os.PathListSeparator) + dirB + string(os.PathListSeparator) + dirC

	got := OtherCopies(pathEnv, current)
	if len(got) != 1 || got[0] != other {
		t.Fatalf("OtherCopies() = %v, want [%q]", got, other)
	}
}

func TestOtherCopiesEmptyWhenOnlyCurrentOnPath(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "moltnet")
	writeExecutable(t, current)

	got := OtherCopies(dir, current)
	if len(got) != 0 {
		t.Fatalf("OtherCopies() = %v, want empty", got)
	}
}

func TestOtherCopiesSkipsNonExecutableFiles(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "moltnet")
	if err := os.WriteFile(candidate, []byte("not executable"), 0o644); err != nil {
		t.Fatalf("write %q: %v", candidate, err)
	}

	got := OtherCopies(dir, filepath.Join(t.TempDir(), "moltnet"))
	if len(got) != 0 {
		t.Fatalf("OtherCopies() = %v, want empty (non-executable file skipped)", got)
	}
}

func TestOtherCopiesDedupesSymlinkToCurrent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}

	realDir := t.TempDir()
	linkDir := t.TempDir()

	current := filepath.Join(realDir, "moltnet")
	writeExecutable(t, current)

	link := filepath.Join(linkDir, "moltnet")
	if err := os.Symlink(current, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	pathEnv := linkDir + string(os.PathListSeparator) + realDir
	got := OtherCopies(pathEnv, current)
	if len(got) != 0 {
		t.Fatalf("OtherCopies() = %v, want empty (symlink resolves to current binary)", got)
	}
}

func TestDanglingSymlinksFindsSymlinkToRemovedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}

	realDir := t.TempDir()
	linkDir := t.TempDir()

	removed := filepath.Join(realDir, "moltnet")
	writeExecutable(t, removed)

	link := filepath.Join(linkDir, "moltnet")
	if err := os.Symlink(removed, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Simulate `moltnet uninstall` having already deleted the binary the
	// symlink points at.
	if err := os.Remove(removed); err != nil {
		t.Fatalf("remove %q: %v", removed, err)
	}

	got := DanglingSymlinks(linkDir, removed)
	if len(got) != 1 || got[0] != link {
		t.Fatalf("DanglingSymlinks() = %v, want [%q]", got, link)
	}
}

func TestDanglingSymlinksIgnoresSymlinkToLiveBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}

	realDir := t.TempDir()
	linkDir := t.TempDir()

	live := filepath.Join(realDir, "moltnet")
	writeExecutable(t, live)

	link := filepath.Join(linkDir, "moltnet")
	if err := os.Symlink(live, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got := DanglingSymlinks(linkDir, filepath.Join(t.TempDir(), "moltnet"))
	if len(got) != 0 {
		t.Fatalf("DanglingSymlinks() = %v, want empty (symlink still resolves to a live binary)", got)
	}
}

func TestDanglingSymlinksIgnoresRegularFiles(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "moltnet"))

	got := DanglingSymlinks(dir, filepath.Join(t.TempDir(), "moltnet"))
	if len(got) != 0 {
		t.Fatalf("DanglingSymlinks() = %v, want empty (not a symlink)", got)
	}
}

func TestDanglingSymlinksEmptyWhenPathHasNoEntry(t *testing.T) {
	dir := t.TempDir()
	got := DanglingSymlinks(dir, filepath.Join(t.TempDir(), "moltnet"))
	if len(got) != 0 {
		t.Fatalf("DanglingSymlinks() = %v, want empty", got)
	}
}

func TestOtherCopiesIgnoresEmptyPathEntries(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "moltnet")
	writeExecutable(t, other)

	pathEnv := "" + string(os.PathListSeparator) + dir + string(os.PathListSeparator) + ""
	got := OtherCopies(pathEnv, filepath.Join(t.TempDir(), "moltnet"))
	if len(got) != 1 || got[0] != other {
		t.Fatalf("OtherCopies() = %v, want [%q]", got, other)
	}
}
