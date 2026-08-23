package main

import (
	"os"
	"path/filepath"
	"testing"
)

// init_checkout_writefileifmissing_test.go continues init_checkout_test.go,
// split out purely to keep that file under this repo's 400-line limit: the
// tests here all cover writeFileIfMissing's own no-clobber/no-op semantics.

// TestWriteFileIfMissingRefusesSymlinkTarget is round 2's P2-3 fix: the
// O_CREATE|O_EXCL rewrite writeFileIfMissing's own doc comment describes had
// zero test coverage -- reverting it to a separate Stat-then-Write (the
// pre-fix TOCTOU-vulnerable shape) broke no test in this repo, the same
// defect class as the coverage gap round 1 was reviewed for, recurring on a
// fix from that very round. This pins the live-secret case directly: a path
// that is itself a symlink to an existing file must be refused --
// (false, nil) -- without ever following the link and overwriting whatever
// it points to.
//
// Mechanics note: this no longer reaches publishNoReplace/O_CREATE|O_EXCL at
// all -- writeFileIfMissing's newer os.Lstat pre-check (init_checkout.go)
// short-circuits first for any already-existing directory entry, symlink
// included, and returns (false, nil) before publishNoReplace is ever
// called. Not a coverage hole: publishNoReplace's own no-clobber semantics
// against a symlinked destination are covered directly at the publish level
// (its own package tests), and the guarantee this test pins -- a symlinked
// path is refused, never followed -- holds regardless of which of the two
// guards is the one that actually catches it.
func TestWriteFileIfMissingRefusesSymlinkTarget(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	created, err := writeFileIfMissing(link, "attacker-controlled\n")
	if err != nil {
		t.Fatalf("writeFileIfMissing() error = %v", err)
	}
	if created {
		t.Fatalf("writeFileIfMissing() created = true, want false for a symlinked path")
	}

	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(contents) != "original\n" {
		t.Fatalf("target contents changed via symlink write: got %q", contents)
	}
}

// TestWriteFileIfMissingRefusesDanglingSymlink covers the dangling-symlink
// variant: the symlink itself exists (there is nothing to follow to), and
// O_CREATE|O_EXCL must still refuse -- POSIX open(2) fails with EEXIST
// whenever path names a symlink at all, dangling or not -- rather than
// creating a fresh regular file at link and silently replacing the
// symlink's semantics.
//
// Mechanics note: same as TestWriteFileIfMissingRefusesSymlinkTarget above
// -- writeFileIfMissing's os.Lstat pre-check now short-circuits this case
// too, before O_CREATE|O_EXCL/publishNoReplace is ever reached. Still
// covered at the publish level; the guarantee itself is unaffected by which
// guard actually catches it.
func TestWriteFileIfMissingRefusesDanglingSymlink(t *testing.T) {
	directory := t.TempDir()
	link := filepath.Join(directory, "dangling")
	if err := os.Symlink(filepath.Join(directory, "does-not-exist"), link); err != nil {
		t.Fatalf("create dangling symlink: %v", err)
	}

	created, err := writeFileIfMissing(link, "contents\n")
	if err != nil {
		t.Fatalf("writeFileIfMissing() error = %v", err)
	}
	if created {
		t.Fatalf("writeFileIfMissing() created = true, want false for a dangling symlink")
	}

	info, lstatErr := os.Lstat(link)
	if lstatErr != nil {
		t.Fatalf("Lstat(link) error = %v", lstatErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected link to remain a symlink, got mode %v", info.Mode())
	}
}

// TestWriteFileIfMissingNoOpNeverCreatesTempFile pins the second half of the
// credential-leak fix (the first half is gitignoreCredentialEntries' new
// ".moltnet-init-*.tmp" entry): the no-op path -- path already exists, the
// common case for a second `moltnet init` against a healthy network -- must
// never create a same-directory ".moltnet-init-*.tmp" staging file at all.
// For a real runInit caller on this path, contents is actually empty (
// serverContents stays "" whenever serverExists is true, init.go) so the
// old behavior was wasted I/O rather than a token leak there -- but this
// test passes a non-empty "freshly-generated-secret\n" deliberately, because
// the pre-check this test pins is also what protects the one call shape
// where contents genuinely does hold a token on this same no-op path: a
// symlinked or directory serverPath, which reads as serverExists == false
// upstream so a fresh token is built anyway. Before this fix,
// writeFileIfMissing always created and wrote the temp file first and only
// discovered the no-op once publishNoReplace ran, so an interruption between
// those two steps (SIGKILL, power loss -- exactly the class the rest of this
// function's atomic-publish machinery exists to survive) could leave a
// secret-bearing temp file on disk in that shape, even though path already
// existed and nothing here was ever meant to write anything.
//
// A plain "does the temp file still exist after the call returns" assertion
// would not actually catch a regression here: writeFileIfMissing's own
// deferred cleanup removes any temp file it created before returning at all
// (the "attacker interrupts the process" window this fix closes is
// mid-function, not post-return), so such an assertion passes whether or not
// this function still creates-then-immediately-deletes the temp file. This
// test instead makes directory read-only (0o500: list and traverse, no
// write) after seeding the pre-existing file, so any attempt to
// os.CreateTemp inside it -- the exact operation the pre-check exists to
// skip -- fails with a permission error that surfaces as a returned error
// instead of the expected (false, nil). A regression that deletes the
// pre-check is caught by that returned error, not by inspecting directory
// contents afterward.
func TestWriteFileIfMissingNoOpNeverCreatesTempFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatalf("chmod directory read-only: %v", err)
	}
	// Restore write permission before t.TempDir()'s own cleanup runs, or
	// removing the directory itself would fail.
	t.Cleanup(func() {
		_ = os.Chmod(directory, 0o700)
	})

	created, err := writeFileIfMissing(path, "freshly-generated-secret\n")
	if err != nil {
		t.Fatalf("writeFileIfMissing() error = %v, want nil -- a regression that skips the existence pre-check would try (and fail) to create a temp file in this now-read-only directory", err)
	}
	if created {
		t.Fatalf("writeFileIfMissing() created = true, want false for an existing path")
	}

	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("chmod directory back to writable: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the pre-existing file in %q, got %v", directory, entries)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read path: %v", err)
	}
	if string(contents) != "existing\n" {
		t.Fatalf("existing contents changed on the no-op path: got %q", contents)
	}
}
