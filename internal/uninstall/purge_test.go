package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPurgeHomeDirRemovesEverything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".moltnet")
	if err := os.MkdirAll(filepath.Join(root, "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "acme", "Moltnet"), []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result, err := PurgeHomeDir(root)
	if err != nil {
		t.Fatalf("PurgeHomeDir() error = %v", err)
	}
	if !result.Existed || result.IsSymlink {
		t.Fatalf("PurgeHomeDir() result = %+v, want Existed=true IsSymlink=false", result)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, stat err = %v", root, err)
	}
}

func TestPurgeHomeDirMissingRootIsNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "does-not-exist")
	result, err := PurgeHomeDir(root)
	if err != nil {
		t.Fatalf("PurgeHomeDir() error = %v", err)
	}
	if result.Existed {
		t.Fatalf("PurgeHomeDir() result = %+v, want Existed=false", result)
	}
}

func TestPurgeHomeDirEmptyRootErrors(t *testing.T) {
	if _, err := PurgeHomeDir(""); err == nil {
		t.Fatal("expected an error for an empty root")
	}
}

// TestPurgeHomeDirSymlinkRemovesOnlyTheLink is the P2-2 regression test: a
// ~/.moltnet that is actually a symlink to real data elsewhere must have
// only the link removed, with the resolved target reported back untouched
// — never RemoveAll'd, and never silently claimed as "the data was
// removed" when only the link was.
func TestPurgeHomeDirSymlinkRemovesOnlyTheLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}

	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".moltnet")
	if err := os.Symlink(target, root); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	result, err := PurgeHomeDir(root)
	if err != nil {
		t.Fatalf("PurgeHomeDir() error = %v", err)
	}
	if !result.Existed || !result.IsSymlink {
		t.Fatalf("PurgeHomeDir() result = %+v, want Existed=true IsSymlink=true", result)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", target, err)
	}
	if result.SymlinkTarget != resolvedTarget {
		t.Fatalf("PurgeHomeDir() SymlinkTarget = %q, want %q", result.SymlinkTarget, resolvedTarget)
	}

	// The link itself is gone...
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("expected symlink %q to be removed, lstat err = %v", root, err)
	}
	// ...but the data at the resolved target survives untouched.
	if _, err := os.Stat(filepath.Join(target, "acme")); err != nil {
		t.Fatalf("expected symlink target data to survive, stat err = %v", err)
	}
}

// TestPurgeHomeDirDanglingSymlinkReportsNoTarget covers a ~/.moltnet
// symlink whose target has already vanished: PurgeHomeDir still removes
// just the link, but SymlinkTarget comes back empty since there is nothing
// meaningful to name.
func TestPurgeHomeDirDanglingSymlinkReportsNoTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}

	goneTarget := filepath.Join(t.TempDir(), "gone")
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".moltnet")
	if err := os.Symlink(goneTarget, root); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	result, err := PurgeHomeDir(root)
	if err != nil {
		t.Fatalf("PurgeHomeDir() error = %v", err)
	}
	if !result.Existed || !result.IsSymlink || result.SymlinkTarget != "" {
		t.Fatalf("PurgeHomeDir() result = %+v, want Existed=true IsSymlink=true SymlinkTarget=\"\"", result)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("expected dangling symlink %q to be removed, lstat err = %v", root, err)
	}
}

// TestPurgeHomeDirRefusesWhenRootIsHome is the guard's core regression case:
// a misresolved MOLTNET_HOME=$HOME must never reach os.RemoveAll, even with
// --purge --yes upstream in the CLI layer skipping every prompt.
func TestPurgeHomeDirRefusesWhenRootIsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := PurgeHomeDir(home)
	if err == nil {
		t.Fatal("PurgeHomeDir(home) error = nil, want a refusal error")
	}
	if !strings.Contains(err.Error(), home) {
		t.Fatalf("PurgeHomeDir(home) error = %q, want it to name %q", err.Error(), home)
	}
	if _, statErr := os.Stat(home); statErr != nil {
		t.Fatalf("expected %q to survive the refused purge, stat err = %v", home, statErr)
	}
}

// TestPurgeHomeDirRefusesFilesystemRoot covers MOLTNET_HOME=/: the
// filesystem-root check is pure path arithmetic, so it must refuse before
// ever touching the disk.
func TestPurgeHomeDirRefusesFilesystemRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := PurgeHomeDir("/")
	if err == nil {
		t.Fatal("PurgeHomeDir(\"/\") error = nil, want a refusal error")
	}
	if !strings.Contains(err.Error(), "/") {
		t.Fatalf("PurgeHomeDir(\"/\") error = %q, want it to name the filesystem root", err.Error())
	}
}

// TestPurgeHomeDirRefusesAncestorOfHome covers a MOLTNET_HOME resolved to a
// directory that merely contains $HOME (e.g. /Users when $HOME is
// /Users/someone) rather than $HOME itself.
func TestPurgeHomeDirRefusesAncestorOfHome(t *testing.T) {
	ancestor := t.TempDir()
	home := filepath.Join(ancestor, "someone")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	_, err := PurgeHomeDir(ancestor)
	if err == nil {
		t.Fatal("PurgeHomeDir(ancestor) error = nil, want a refusal error")
	}
	if !strings.Contains(err.Error(), ancestor) {
		t.Fatalf("PurgeHomeDir(ancestor) error = %q, want it to name %q", err.Error(), ancestor)
	}
	if _, statErr := os.Stat(home); statErr != nil {
		t.Fatalf("expected %q to survive the refused purge, stat err = %v", home, statErr)
	}
}

// TestPurgeHomeDirPurgesLegitOverride is the guard's negative case: a
// MOLTNET_HOME override that lives entirely outside $HOME — the normal
// shape of a legitimate override — must still purge exactly as before.
func TestPurgeHomeDirPurgesLegitOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	override := filepath.Join(t.TempDir(), "moltnet-home-override")
	if err := os.MkdirAll(filepath.Join(override, "acme"), 0o700); err != nil {
		t.Fatalf("mkdir override: %v", err)
	}

	result, err := PurgeHomeDir(override)
	if err != nil {
		t.Fatalf("PurgeHomeDir(override) error = %v", err)
	}
	if !result.Existed || result.IsSymlink {
		t.Fatalf("PurgeHomeDir(override) result = %+v, want Existed=true IsSymlink=false", result)
	}
	if _, statErr := os.Stat(override); !os.IsNotExist(statErr) {
		t.Fatalf("expected %q to be removed, stat err = %v", override, statErr)
	}
}

func TestInspectHomeDirMatchesPurgeHomeDirBeforeRemoval(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".moltnet")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	state, err := InspectHomeDir(root)
	if err != nil {
		t.Fatalf("InspectHomeDir() error = %v", err)
	}
	if !state.Existed || state.IsSymlink {
		t.Fatalf("InspectHomeDir() = %+v, want Existed=true IsSymlink=false", state)
	}
}
