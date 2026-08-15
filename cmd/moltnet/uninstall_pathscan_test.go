package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunUninstallCommandWarnsAboutDanglingSymlink is the P2-1 regression
// test: a PATH entry that is a symlink to the moltnet binary being deleted
// must be reported as a dangling symlink after self-deletion, instead of
// silently disappearing from every warning the way it did when OtherCopies
// (os.Stat-based) was the only PATH scan.
func TestRunUninstallCommandWarnsAboutDanglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	binaryPath := withScratchExecutable(t)

	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "moltnet")
	if err := os.Symlink(binaryPath, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Setenv("PATH", linkDir)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if strings.Contains(output, "warning: other moltnet copies remain on PATH:") {
		t.Fatalf("output = %q, want the symlink-to-self not reported as a live other copy", output)
	}
	wantWarning := "warning: dangling moltnet symlinks remain on PATH:"
	wantLine := "dangling symlink: " + link + " — remove it with rm " + link
	if !strings.Contains(output, wantWarning) || !strings.Contains(output, wantLine) {
		t.Fatalf("output = %q, want it to contain %q and %q", output, wantWarning, wantLine)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("expected the dangling symlink itself to survive (uninstall only warns, never deletes it): %v", err)
	}
}
