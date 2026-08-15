package main

import (
	"context"
	"fmt"
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
	// (service never installed) — uninstall must catch both, but P1 means
	// only "acme" gets a truthful ✓; see
	// TestRunUninstallCommandNoServiceNetworkGetsNoFalseCheck for the
	// dedicated no-service-at-all regression.
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
		`stopped and removed the service for network "acme"`,
		"removed " + binaryPath,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want it to contain %q", output, want)
		}
	}
	// P1: "beta" never had a service installed, so it must get neither a
	// plan bullet nor a completion ✓ — manager.Uninstall is a truthful
	// no-op for it, and the output must agree.
	for _, notWant := range []string{
		`stop and remove the service for network "beta"`,
		`stopped and removed the service for network "beta"`,
	} {
		if strings.Contains(output, notWant) {
			t.Fatalf("output = %q, want it to not contain %q (beta never had an installed service)", output, notWant)
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

// TestRunUninstallCommandNoServiceNetworkGetsNoFalseCheck is the P1
// regression test for the common `moltnet init` (without `service install`)
// path: a network with only a ~/.moltnet/<id>/ directory and no installed
// unit/plist must get neither a "stop and remove the service" plan bullet
// nor a "stopped and removed" ✓ line — both would be untruthful, since
// manager.Uninstall is a no-op for it. The plan must instead fall back to
// the same "no installed services found" note the fully-empty case prints,
// because zero of the networks it lists have an actual service to stop.
func TestRunUninstallCommandNoServiceNetworkGetsNoFalseCheck(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	binaryPath := withScratchExecutable(t)

	// "beta" has a network directory but its service was never installed —
	// e.g. `moltnet init` ran without a follow-up `moltnet service install`.
	betaDir := filepath.Join(home, ".moltnet", "beta")
	if err := os.MkdirAll(betaDir, 0o700); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "no installed services found") {
		t.Fatalf("output = %q, want a no-installed-services note even though a network directory exists", output)
	}
	for _, notWant := range []string{
		`stop and remove the service for network "beta"`,
		`stopped and removed the service for network "beta"`,
	} {
		if strings.Contains(output, notWant) {
			t.Fatalf("output = %q, want it to not contain %q (beta never had an installed service)", output, notWant)
		}
	}
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, stat err = %v", binaryPath, err)
	}
	// Data survives by default.
	if _, err := os.Stat(betaDir); err != nil {
		t.Fatalf("expected %q to still exist without --purge: %v", betaDir, err)
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

// uninstallSummaryInvariantCase renders the full `moltnet uninstall
// --purge` aftercare sequence — plan, service checkmark, purge warning
// block, purge result, and binary checkmark — against fixed fake data via
// the same pure print/build helpers production code calls, so the case is
// identical on every invocation regardless of isOutputTerminal. It mirrors
// style_test.go's alignmentInvariantCase, adapted to the uninstall summary
// path (printUninstallPlan, printUninstallCheck, buildPurgeConfirmationPrompt,
// printPurgeResult) instead of printInitSummary/printNextSteps.
func uninstallSummaryInvariantCase(t *testing.T) string {
	t.Helper()
	return captureStdout(t, func() {
		fmt.Fprint(stdout, "  Uninstalling moltnet\n\n")
		printUninstallPlan([]string{"acme-friends"}, map[string]bool{"acme-friends": true}, "/home/example/.local/bin/moltnet", true, "", false)
		fmt.Fprintln(stdout)
		printUninstallCheck("stopped and removed the service for network %q", "acme-friends")
		fmt.Fprintln(stdout)
		fmt.Fprint(stdout, buildPurgeConfirmationPrompt([]string{"acme-friends"}, "/home/example/.moltnet", uninstall.HomeState{Existed: true}, "", false))
		fmt.Fprintln(stdout)
		printPurgeResult("/home/example/.moltnet", uninstall.PurgeResult{HomeState: uninstall.HomeState{Existed: true}})
		printUninstallCheck("removed %s", "/home/example/.local/bin/moltnet")
	})
}

// TestUninstallSummaryIsPlainWidthInvariant is the uninstall counterpart to
// style_test.go's TestAlignmentIsPlainWidthInvariant: it forces the
// isOutputTerminal seam to true (NO_COLOR unset, TERM color-capable),
// captures the uninstall summary's styled output, strips every ANSI escape
// code, and asserts the result is byte-identical to the same calls' plain
// (non-TTY) output — i.e. the new Plan:/✓/warning: structure adds only
// color on a terminal, never a text-content change.
func TestUninstallSummaryIsPlainWidthInvariant(t *testing.T) {
	previousNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatalf("unset NO_COLOR: %v", err)
	}
	t.Cleanup(func() {
		if hadNoColor {
			os.Setenv("NO_COLOR", previousNoColor)
		} else {
			os.Unsetenv("NO_COLOR")
		}
	})
	t.Setenv("TERM", "xterm-256color")

	previousTerminal := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	styledOutput := uninstallSummaryInvariantCase(t)
	isOutputTerminal = previousTerminal

	if !strings.ContainsRune(styledOutput, '\x1b') {
		t.Fatalf("styledOutput = %q, want at least one ANSI escape code from the TTY-styled path", styledOutput)
	}

	plainOutput := uninstallSummaryInvariantCase(t)
	if strings.ContainsRune(plainOutput, '\x1b') {
		t.Fatalf("plainOutput = %q, want no ANSI escape codes from the non-TTY default path", plainOutput)
	}

	strippedStyled := ansiEscapePattern.ReplaceAllString(styledOutput, "")
	if strippedStyled != plainOutput {
		t.Fatalf("ANSI-stripped styled output does not byte-match plain output:\nstripped styled = %q\nplain           = %q", strippedStyled, plainOutput)
	}
}
