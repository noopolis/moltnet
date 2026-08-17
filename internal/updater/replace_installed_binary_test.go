package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceInstalledBinaryRefusesUnwritableDirectory(t *testing.T) {
	directory := t.TempDir()
	installPath := filepath.Join(directory, "moltnet")
	if err := os.WriteFile(installPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("write install path: %v", err)
	}
	newBinary := filepath.Join(t.TempDir(), "moltnet")
	if err := os.WriteFile(newBinary, []byte("new"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatalf("make install directory read-only: %v", err)
	}
	defer os.Chmod(directory, 0o755)

	_, err := replaceInstalledBinary(installPath, newBinary)
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("expected writability refusal naming a fix, got %v", err)
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Fatalf("expected refusal to name an exact fix command, got %v", err)
	}

	contents, readErr := os.ReadFile(installPath)
	if readErr != nil {
		t.Fatalf("read install path: %v", readErr)
	}
	if string(contents) != "old" {
		t.Fatalf("install path mutated despite refusal: %q", contents)
	}
}

func TestReplaceInstalledBinaryPreservesExecutableBitAndBacksUpOld(t *testing.T) {
	directory := t.TempDir()
	installPath := filepath.Join(directory, "moltnet")
	if err := os.WriteFile(installPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("write install path: %v", err)
	}
	newBinary := filepath.Join(t.TempDir(), "moltnet")
	if err := os.WriteFile(newBinary, []byte("new"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	backupPath, err := replaceInstalledBinary(installPath, newBinary)
	if err != nil {
		t.Fatalf("replaceInstalledBinary: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected a non-empty backup path")
	}

	info, err := os.Stat(installPath)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable bit not preserved: %o", info.Mode().Perm())
	}

	backupContents, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupContents) != "old" {
		t.Fatalf("backup contents = %q, want %q", backupContents, "old")
	}
	installedContents, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(installedContents) != "new" {
		t.Fatalf("installed contents = %q, want %q", installedContents, "new")
	}
}

// TestReplaceInstalledBinaryPreservesInstalledModeNotBuiltMode is the
// regression test for the P3 fix: the installed binary's own executable
// mode (which an operator may have chmod'd deliberately) must survive a
// replace, even when the freshly built file carries a different mode.
func TestReplaceInstalledBinaryPreservesInstalledModeNotBuiltMode(t *testing.T) {
	directory := t.TempDir()
	installPath := filepath.Join(directory, "moltnet")
	if err := os.WriteFile(installPath, []byte("old"), 0o700); err != nil {
		t.Fatalf("write install path: %v", err)
	}
	newBinary := filepath.Join(t.TempDir(), "moltnet")
	if err := os.WriteFile(newBinary, []byte("new"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	if _, err := replaceInstalledBinary(installPath, newBinary); err != nil {
		t.Fatalf("replaceInstalledBinary: %v", err)
	}

	info, err := os.Stat(installPath)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("installed mode = %o, want the preserved installed mode %o (built file was %o)", got, 0o700, 0o755)
	}
}

// TestReplaceInstalledBinaryLeavesInstallPathIntactWhenBackupFails is the
// regression test for P2-8: the old rename(install->.previous) then
// rename(staged->install) sequence unlinked installPath as its very first
// mutating step, so any failure afterward — including a crash — left
// nothing runnable there. The fixed version copies installPath to the
// backup location instead (installPath itself is never renamed away, only
// ever replaced by one final atomic rename), so a failure at the backup
// step must leave installPath completely untouched, not merely restorable.
func TestReplaceInstalledBinaryLeavesInstallPathIntactWhenBackupFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a permission-based backup failure cannot be simulated")
	}
	directory := t.TempDir()
	installPath := filepath.Join(directory, "moltnet")
	if err := os.WriteFile(installPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("write install path: %v", err)
	}
	newBinary := filepath.Join(t.TempDir(), "moltnet")
	if err := os.WriteFile(newBinary, []byte("new"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	// Pre-create the backup destination as read-only, so copyRegularFile's
	// O_WRONLY open of it fails — simulating a backup-step failure without
	// needing to inject a crash mid-function.
	backupPath := installPath + ".previous"
	if err := os.WriteFile(backupPath, []byte("stale"), 0o400); err != nil {
		t.Fatalf("seed read-only backup path: %v", err)
	}
	defer os.Chmod(backupPath, 0o600)

	if _, err := replaceInstalledBinary(installPath, newBinary); err == nil {
		t.Fatal("expected backup failure to propagate as an error")
	}

	contents, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatalf("installPath missing after failed backup: %v", err)
	}
	if string(contents) != "old" {
		t.Fatalf("installPath mutated despite backup failure: %q", contents)
	}
}
