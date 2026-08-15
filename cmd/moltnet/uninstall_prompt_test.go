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

// TestRunUninstallCommandPurgePromptNoServicesHasSingleBlankLine is a
// spacing regression test: with zero installed services, the unconditional
// blank line printed right after "Proceed? [y/N]" (runUninstallCommand) and
// the leading blank line runUninstallPurge used to print unconditionally
// before its own warning prompt used to collide with nothing in between —
// two Fprintln(stdout) calls back to back — producing a doubled blank line
// once a real terminal's own echoed newline is added on top (visible as
// "\n\n\n" between the answered prompt and the purge warning). With services
// installed, the ✓ line(s) printed by the uninstall loop sit between those
// two blanks and the spacing is correct as-is; this test pins the
// zero-services case, where hadServiceOutput now suppresses the second,
// redundant blank.
func TestRunUninstallCommandPurgePromptNoServicesHasSingleBlankLine(t *testing.T) {
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "")
	withScratchExecutable(t)

	withPromptAnswers(t, "y", "y")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--purge"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	const promptMarker = "Proceed? [y/N] "
	promptIdx := strings.Index(output, promptMarker)
	if promptIdx < 0 {
		t.Fatalf("output = %q, want it to contain the main confirmation prompt", output)
	}
	after := output[promptIdx+len(promptMarker):]
	warningIdx := strings.Index(after, "warning:")
	if warningIdx < 0 {
		t.Fatalf("output = %q, want it to contain the purge warning prompt", output)
	}
	between := after[:warningIdx]

	// The literal regression this test guards: no doubled blank line between
	// the main prompt and the purge warning.
	if strings.Contains(between, "\n\n\n") {
		t.Fatalf("output between the main prompt and purge warning = %q, want no doubled blank line (no \"\\n\\n\\n\")", between)
	}
	// Stronger pin: in this zero-services case the two sections are adjacent
	// with no blank line at all between them — any blank line here at all
	// would mean the redundant Fprintln crept back in.
	if strings.Contains(between, "\n\n") {
		t.Fatalf("output between the main prompt and purge warning = %q, want no blank line at all when no services were printed", between)
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

	planIdx := strings.Index(output, "Plan:")
	mutationIdx := strings.Index(output, `stopped and removed the service for network "acme"`)
	if planIdx < 0 || mutationIdx < 0 {
		t.Fatalf("output = %q, want both plan text and mutation output present", output)
	}
	if planIdx > mutationIdx {
		t.Fatalf("output = %q, want plan text printed before mutation output", output)
	}
}
