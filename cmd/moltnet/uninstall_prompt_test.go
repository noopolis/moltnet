package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/service"
)

// TestRunUninstallCommandMainPromptNoMutatesNothing is the P2-3 regression
// test: declining the first confirmation prompt must leave every service,
// the network directory, and the binary itself untouched.
func TestRunUninstallCommandMainPromptNoMutatesNothing(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	binaryPath := withScratchExecutable(t)

	acmeDir := filepath.Join(home, ".moltnet", "acme")
	if err := os.MkdirAll(acmeDir, 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}
	mgr := service.NewForOS(fakeServiceRunner{}, "linux")
	spec := service.Spec{
		NetworkID:  "acme",
		ConfigPath: filepath.Join(acmeDir, "Moltnet"),
		BinaryPath: binaryPath,
		NetworkDir: acmeDir,
	}
	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	withPromptAnswers(t, "n")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "aborted; nothing was changed") {
		t.Fatalf("output = %q, want an aborted note", output)
	}
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("expected binary to survive a declined prompt: %v", err)
	}
	if _, err := os.Stat(acmeDir); err != nil {
		t.Fatalf("expected %q to survive a declined prompt: %v", acmeDir, err)
	}
	unitPath, err := service.SystemdUnitPath("acme")
	if err != nil {
		t.Fatalf("SystemdUnitPath() error = %v", err)
	}
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("expected service unit to survive a declined prompt: %v", err)
	}
}

// TestRunUninstallCommandPurgePromptNoLeavesHomeDirIntact is the P2-3
// regression test for the second confirmation: 'y' at the main prompt lets
// services and the binary get removed exactly as --yes alone would, but
// 'n' at the purge-specific prompt must leave ~/.moltnet in place.
func TestRunUninstallCommandPurgePromptNoLeavesHomeDirIntact(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	binaryPath := withScratchExecutable(t)

	acmeDir := filepath.Join(home, ".moltnet", "acme")
	if err := os.MkdirAll(acmeDir, 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}
	mgr := service.NewForOS(fakeServiceRunner{}, "linux")
	spec := service.Spec{
		NetworkID:  "acme",
		ConfigPath: filepath.Join(acmeDir, "Moltnet"),
		BinaryPath: binaryPath,
		NetworkDir: acmeDir,
	}
	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	withPromptAnswers(t, "y", "n")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--purge"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "--purge aborted") {
		t.Fatalf("output = %q, want a purge-aborted note", output)
	}
	// Services and the binary follow the main-prompt "yes" — unaffected by
	// the later purge-prompt "no".
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("expected binary to be removed after a 'yes' main prompt, stat err = %v", err)
	}
	unitPath, err := service.SystemdUnitPath("acme")
	if err != nil {
		t.Fatalf("SystemdUnitPath() error = %v", err)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected service unit to be removed after a 'yes' main prompt, stat err = %v", err)
	}
	// ~/.moltnet survives: the purge-specific prompt was declined.
	if _, err := os.Stat(acmeDir); err != nil {
		t.Fatalf("expected %q to survive a declined purge prompt: %v", acmeDir, err)
	}
}

// TestRunUninstallCommandPrintsPlanBeforeMutating is the P2-3 regression
// test for ordering: the plan text must appear in stdout before any
// mutation output, so an operator reading along never sees an action
// happen before its description.
func TestRunUninstallCommandPrintsPlanBeforeMutating(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	binaryPath := withScratchExecutable(t)

	acmeDir := filepath.Join(home, ".moltnet", "acme")
	if err := os.MkdirAll(acmeDir, 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}
	mgr := service.NewForOS(fakeServiceRunner{}, "linux")
	spec := service.Spec{
		NetworkID:  "acme",
		ConfigPath: filepath.Join(acmeDir, "Moltnet"),
		BinaryPath: binaryPath,
		NetworkDir: acmeDir,
	}
	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	planIdx := strings.Index(output, "moltnet uninstall will:")
	mutationIdx := strings.Index(output, `stopped and removed the moltnet service for network "acme"`)
	if planIdx < 0 || mutationIdx < 0 {
		t.Fatalf("output = %q, want both plan text and mutation output present", output)
	}
	if planIdx > mutationIdx {
		t.Fatalf("output = %q, want plan text printed before mutation output", output)
	}
}
