package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/service"
	"github.com/noopolis/moltnet/internal/uninstall"
)

// withPromptAnswers scripts promptYesNo's answers for the test's duration —
// one line per confirmation prompt, in order — and forces isInteractive()
// to true so `moltnet uninstall`'s confirmation gates are actually reached
// without a real terminal attached to stdin. Each line is wired up as its
// own io.Reader via io.MultiReader rather than one shared multi-line
// reader, so a fresh bufio.Reader created per promptYesNo call (the
// production pattern; see uninstall.go) only ever pulls in the one line it
// should, matching how a real line-buffered terminal delivers input.
func withPromptAnswers(t *testing.T, lines ...string) {
	t.Helper()
	readers := make([]io.Reader, len(lines))
	for i, line := range lines {
		readers[i] = strings.NewReader(line + "\n")
	}

	previousReader := promptReader
	promptReader = io.MultiReader(readers...)
	t.Cleanup(func() { promptReader = previousReader })

	previousInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = previousInteractive })
}

// withScratchExecutable copies content into a throwaway file in a fresh
// temp dir, points currentExecutable at it for the test's duration, and
// returns its path — so `moltnet uninstall` tests exercise real self-
// deletion without ever touching the actual go test binary.
func withScratchExecutable(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "moltnet")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write scratch executable: %v", err)
	}

	previous := currentExecutable
	currentExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { currentExecutable = previous })

	return path
}

func TestRunUninstallCommandYesRemovesServicesAndBinary(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	binaryPath := withScratchExecutable(t)

	// "acme" has an installed service; "beta" only a network directory
	// (service never installed) — uninstall must catch both.
	acmeDir := filepath.Join(home, ".moltnet", "acme")
	if err := os.MkdirAll(acmeDir, 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".moltnet", "beta"), 0o700); err != nil {
		t.Fatalf("mkdir beta: %v", err)
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

	for _, want := range []string{
		`stopped and removed the moltnet service for network "acme"`,
		`stopped and removed the moltnet service for network "beta"`,
		"removed " + binaryPath,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want it to contain %q", output, want)
		}
	}

	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, stat err = %v", binaryPath, err)
	}
	unitPath, err := service.SystemdUnitPath("acme")
	if err != nil {
		t.Fatalf("SystemdUnitPath() error = %v", err)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, stat err = %v", unitPath, err)
	}
	// Data survives by default.
	if _, err := os.Stat(acmeDir); err != nil {
		t.Fatalf("expected %q to still exist without --purge: %v", acmeDir, err)
	}
}

func TestRunUninstallCommandNoNetworksStillDeletesBinary(t *testing.T) {
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "")
	binaryPath := withScratchExecutable(t)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "no installed services found") {
		t.Fatalf("output = %q, want a no-installed-services note", output)
	}
	if strings.Contains(output, "stop and remove no services") {
		t.Fatalf("output = %q, want the empty-state bullet dropped", output)
	}
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, stat err = %v", binaryPath, err)
	}
}

func TestRunUninstallCommandRequiresYesWithoutTTY(t *testing.T) {
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	withScratchExecutable(t)

	var err error
	captureStdout(t, func() {
		err = run(context.Background(), []string{"uninstall"}, "test")
	})
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("uninstall error = %v, want a requires --yes error", err)
	}
}

func TestRunUninstallCommandPurgeYesRemovesHomeDir(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	withScratchExecutable(t)

	root := filepath.Join(home, ".moltnet")
	if err := os.MkdirAll(filepath.Join(root, "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes", "--purge"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "removed "+root) {
		t.Fatalf("output = %q, want it to report removing %q", output, root)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, stat err = %v", root, err)
	}
}

func TestRunUninstallCommandWithoutPurgeLeavesHomeDir(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	withScratchExecutable(t)

	root := filepath.Join(home, ".moltnet")
	if err := os.MkdirAll(filepath.Join(root, "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}

	captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("expected %q to survive without --purge: %v", root, err)
	}
}

func TestBuildPurgeConfirmationPromptListsNetworkIDs(t *testing.T) {
	root := "/home/example/.moltnet"
	got := buildPurgeConfirmationPrompt([]string{"acme", "beta"}, root, uninstall.HomeState{Existed: true}, "", false)
	if !strings.Contains(got, "acme, beta") {
		t.Fatalf("buildPurgeConfirmationPrompt() = %q, want it to list network ids", got)
	}

	empty := buildPurgeConfirmationPrompt(nil, root, uninstall.HomeState{Existed: true}, "", false)
	if !strings.Contains(empty, "no network data found under it") || !strings.Contains(empty, "the directory itself will be removed") {
		t.Fatalf("buildPurgeConfirmationPrompt(nil) = %q, want the no-network-data wording", empty)
	}
	// P3-3: the first line names root once, not twice.
	firstLine, _, _ := strings.Cut(empty, "\n")
	if strings.Count(firstLine, root) != 1 {
		t.Fatalf("buildPurgeConfirmationPrompt(nil) first line = %q, want %q to appear exactly once (no repeated root)", firstLine, root)
	}

	// When the directory does not exist at all, the prompt must not promise
	// to remove it — it should say plainly that there is nothing there yet.
	missing := buildPurgeConfirmationPrompt(nil, root, uninstall.HomeState{}, "", false)
	if !strings.Contains(missing, "no network data found under it") || !strings.Contains(missing, "nothing exists there yet") {
		t.Fatalf("buildPurgeConfirmationPrompt(nil) = %q, want the nothing-exists-yet wording", missing)
	}
	if strings.Contains(missing, "the directory itself will be removed") {
		t.Fatalf("buildPurgeConfirmationPrompt(nil) = %q, want it not to promise removal of a directory that does not exist", missing)
	}
}

// TestBuildPurgeConfirmationPromptNamesSymlinkTarget is the P2-2 regression
// test: when ~/.moltnet is itself a symlink, the confirmation prompt must
// name the resolved target and say plainly that --purge only removes the
// link, before the operator answers.
func TestBuildPurgeConfirmationPromptNamesSymlinkTarget(t *testing.T) {
	root := "/home/example/.moltnet"
	state := uninstall.HomeState{Existed: true, IsSymlink: true, SymlinkTarget: "/mnt/data/moltnet-home"}
	got := buildPurgeConfirmationPrompt(nil, root, state, "", false)
	if !strings.Contains(got, "is a symlink to /mnt/data/moltnet-home") || !strings.Contains(got, "only removes the link") {
		t.Fatalf("buildPurgeConfirmationPrompt() = %q, want it to name the symlink target", got)
	}
}

// TestBuildPurgeConfirmationPromptMentionsMoltnetHomeOverride is the P2-4
// regression test: when MOLTNET_HOME points install state outside
// ~/.moltnet, the purge confirmation must name that path too.
func TestBuildPurgeConfirmationPromptMentionsMoltnetHomeOverride(t *testing.T) {
	got := buildPurgeConfirmationPrompt(nil, "/home/example/.moltnet", uninstall.HomeState{}, "/mnt/state/moltnet", true)
	if !strings.Contains(got, "MOLTNET_HOME is set") || !strings.Contains(got, "/mnt/state/moltnet") {
		t.Fatalf("buildPurgeConfirmationPrompt() = %q, want it to mention the MOLTNET_HOME override", got)
	}
}

func TestRunUninstallCommandPermissionDeniedFallback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permission checks")
	}
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "")

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "moltnet")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write scratch executable: %v", err)
	}
	previous := currentExecutable
	currentExecutable = func() (string, error) { return binaryPath, nil }
	t.Cleanup(func() { currentExecutable = previous })

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod %q: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "permission denied") || !strings.Contains(output, "sudo rm "+binaryPath) {
		t.Fatalf("output = %q, want a permission-denied sudo fallback", output)
	}
}

func TestRunUninstallCommandWarnsAboutOtherPathCopies(t *testing.T) {
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	withScratchExecutable(t)

	otherDir := t.TempDir()
	otherCopy := filepath.Join(otherDir, "moltnet")
	if err := os.WriteFile(otherCopy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write other copy: %v", err)
	}
	t.Setenv("PATH", otherDir)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "warning: other moltnet copies remain on PATH:") || !strings.Contains(output, otherCopy) {
		t.Fatalf("output = %q, want a PATH warning naming %q", output, otherCopy)
	}
}

func TestRunUninstallCommandHelp(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "help"}, "test"); err != nil {
			t.Fatalf("uninstall help error = %v", err)
		}
	})
	if !strings.Contains(output, "moltnet uninstall") {
		t.Fatalf("unexpected help output %q", output)
	}
}

func TestRunUninstallCommandRejectsPositionalArgs(t *testing.T) {
	err := run(context.Background(), []string{"uninstall", "extra"}, "test")
	if err == nil {
		t.Fatal("expected an error for a positional argument")
	}
}
