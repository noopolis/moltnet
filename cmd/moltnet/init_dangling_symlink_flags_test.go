package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunInitListenAndRoomOnDanglingSymlinkPrintsNoteNotWarning pins round
// 3's serverOccupied fix: a dangling symlink at serverPath used to read as
// serverExists == false (only a plain regular file counts there), so
// `init --listen 0.0.0.0:9922 --room ops` against one took the "fresh"
// branch entirely -- it printed the non-loopback security warning for a
// config it never actually got to write (writeFileIfMissing's own Lstat
// pre-check refuses to publish over any existing directory entry,
// including a dangling symlink), printed no ignored-flags note at all
// (that branch was gated on serverExists too), and minted a throwaway
// operator token. Live-verified exactly as described here. Folding
// serverIsSymlink into the gate must instead print the ignored-flags note
// and suppress the fresh-path warning, matching how writeFileIfMissing was
// always going to treat this target.
func TestRunInitListenAndRoomOnDanglingSymlinkPrintsNoteNotWarning(t *testing.T) {
	directory := t.TempDir()
	danglingTarget := filepath.Join(t.TempDir(), "does-not-exist")
	serverPath := filepath.Join(directory, "Moltnet")
	if err := os.Symlink(danglingTarget, serverPath); err != nil {
		t.Fatalf("create dangling symlink: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "0.0.0.0:9922", "--room", "ops"}); err != nil {
			t.Fatalf("runInit() error = %v, want a graceful degrade for a dangling symlink target", err)
		}
	})

	if !strings.Contains(output, "note:") || !strings.Contains(output, "--listen") || !strings.Contains(output, "--room") {
		t.Fatalf("expected the ignored-flags note naming both --listen and --room, got %q", output)
	}
	if strings.Contains(output, "reachable from outside this machine") {
		t.Fatalf("expected no non-loopback security warning for a config that was never actually written, got %q", output)
	}

	info, lstatErr := os.Lstat(serverPath)
	if lstatErr != nil {
		t.Fatalf("Lstat(serverPath) error = %v", lstatErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected the dangling symlink to remain a symlink (never followed/replaced), got mode %v", info.Mode())
	}
}
