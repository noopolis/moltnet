package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPublishNoReplaceCreatesDestination covers the plain success path for
// publishNoReplace (init_checkout_publish_linux.go /
// init_checkout_publish_darwin.go): a tempPath with no existing destPath
// publishes cleanly, destPath ends up holding tempPath's exact content, and
// tempPath itself no longer exists under its original name afterward
// (consumed either by rename, on Linux, or link+unlink, on Darwin).
func TestPublishNoReplaceCreatesDestination(t *testing.T) {
	directory := t.TempDir()
	tempPath := filepath.Join(directory, ".tmp-publish-src")
	if err := os.WriteFile(tempPath, []byte("fresh content\n"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	destPath := filepath.Join(directory, "dest")

	published, err := publishNoReplace(tempPath, destPath)
	if err != nil {
		t.Fatalf("publishNoReplace() error = %v", err)
	}
	if !published {
		t.Fatal("expected publishNoReplace() to report published = true for a free destination")
	}

	contents, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read destPath: %v", err)
	}
	if string(contents) != "fresh content\n" {
		t.Fatalf("destPath contents = %q, want %q", contents, "fresh content\n")
	}

	if _, statErr := os.Lstat(tempPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected tempPath to no longer exist after a successful publish, stat error = %v", statErr)
	}
}

// TestPublishNoReplaceRefusesConcurrentDestination is the direct regression
// test for PLAN.md's setup-wizard requirement: "test the case where the
// destination appears between write and publication." It simulates exactly
// that race by having destPath already exist by the time publishNoReplace
// runs (indistinguishable, from publishNoReplace's point of view, from a
// concurrent writer having created it a moment earlier) and asserts the
// existing content survives untouched -- the whole reason
// writeFileIfMissing (init_checkout.go) needs a no-replace publish rather
// than a plain temp+os.Rename, which would silently clobber it on Darwin.
func TestPublishNoReplaceRefusesConcurrentDestination(t *testing.T) {
	directory := t.TempDir()
	tempPath := filepath.Join(directory, ".tmp-publish-src")
	if err := os.WriteFile(tempPath, []byte("new content\n"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	destPath := filepath.Join(directory, "dest")
	if err := os.WriteFile(destPath, []byte("existing content\n"), 0o600); err != nil {
		t.Fatalf("write destPath: %v", err)
	}

	published, err := publishNoReplace(tempPath, destPath)
	if err != nil {
		t.Fatalf("publishNoReplace() error = %v", err)
	}
	if published {
		t.Fatal("expected publishNoReplace() to report published = false for an already-existing destination")
	}

	contents, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read destPath: %v", err)
	}
	if string(contents) != "existing content\n" {
		t.Fatalf("destPath was clobbered: got %q, want %q", contents, "existing content\n")
	}
}

// TestPublishNoReplaceRefusesSymlinkDestination covers the same no-replace
// guarantee against a symlinked destination (dangling or not) -- the exact
// property TestWriteFileIfMissingRefusesSymlinkTarget and
// TestWriteFileIfMissingRefusesDanglingSymlink (init_checkout_test.go)
// depend on continuing to hold once writeFileIfMissing moved from a single
// O_CREATE|O_EXCL open to this same-directory-temp-plus-publish shape:
// link()/renameat2(RENAME_NOREPLACE) both check for an existing directory
// entry at the new name without ever following it, so a symlink there --
// pointing anywhere, or nowhere -- refuses exactly like a plain existing
// file does.
func TestPublishNoReplaceRefusesSymlinkDestination(t *testing.T) {
	directory := t.TempDir()
	tempPath := filepath.Join(directory, ".tmp-publish-src")
	if err := os.WriteFile(tempPath, []byte("new content\n"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	destPath := filepath.Join(directory, "dest-link")
	if err := os.Symlink(target, destPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	published, err := publishNoReplace(tempPath, destPath)
	if err != nil {
		t.Fatalf("publishNoReplace() error = %v", err)
	}
	if published {
		t.Fatal("expected publishNoReplace() to report published = false for a symlinked destination")
	}

	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read symlink target: %v", err)
	}
	if string(contents) != "original\n" {
		t.Fatalf("symlink target was clobbered: got %q", contents)
	}
}

// TestWriteFileIfMissingCleansUpTempFileOnDirectorySyncFailure and similar
// low-level failure injection are not covered here: syncDir/tempFile.Sync
// failures require a filesystem that can be made to fail fsync on demand,
// which t.TempDir() (backed by the real filesystem) cannot simulate
// portably. TestWriteFileIfMissing (init_validate_test.go) and the two
// symlink-refusal tests above already exercise writeFileIfMissing's
// observable success/no-op/refusal contract end to end through the real
// publishNoReplace implementation for this OS.
func TestWriteFileIfMissingLeavesNoStrayTempFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")

	if _, err := writeFileIfMissing(path, "version: moltnet.v1\n"); err != nil {
		t.Fatalf("writeFileIfMissing() error = %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "Moltnet" {
		names := make([]string, len(entries))
		for index, entry := range entries {
			names[index] = entry.Name()
		}
		t.Fatalf("expected exactly one file named Moltnet, got %v", names)
	}
}
