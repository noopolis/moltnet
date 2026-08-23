package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteFileIfMissingSyncDirFailureIsNonFatal pins the fix for a real
// misreport: a syncDir failure used to return (false, err) even after
// publishNoReplace had already durably written path, so runInit aborted
// with an error and never reached printInitSummary -- misreporting a write
// that actually happened as though it never occurred, and leaving the
// caller's own "created" bookkeeping wrong on the way out. The directory
// entry fsync is best-effort insurance against a narrow crash window, not a
// correctness requirement for the write itself, so a failure here must be
// logged and the call must still report success.
func TestWriteFileIfMissingSyncDirFailureIsNonFatal(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")

	previousSyncDir := syncDir
	syncDir = func(dir string) error {
		return os.ErrPermission
	}
	t.Cleanup(func() { syncDir = previousSyncDir })

	output := captureStdout(t, func() {
		created, err := writeFileIfMissing(path, "contents\n")
		if err != nil {
			t.Fatalf("writeFileIfMissing() error = %v, want nil even when syncDir fails", err)
		}
		if !created {
			t.Fatalf("writeFileIfMissing() created = false, want true: the file was durably published regardless of the directory-entry fsync outcome")
		}
	})

	if !strings.Contains(output, "note:") || !strings.Contains(output, "sync directory") {
		t.Fatalf("expected a note: line reporting the syncDir failure, got %q", output)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read path: %v", err)
	}
	if string(contents) != "contents\n" {
		t.Fatalf("path contents = %q, want %q", contents, "contents\n")
	}
}
