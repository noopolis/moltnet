package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file holds the regression tests for the source-update fix round
// (fetch-before-compare, global install-metadata isolation, refusal-reason
// dedup, the writability preflight, and the stamped-checkout dir pin). It
// shares the fakeSourceRunner harness (sourceupdate_fake_runner_test.go) and
// helpers (writeMoltnetScript, runVersion, sourceUpdateOptions) with
// sourceupdate_test.go, split into its own file only to stay under this
// package's 400-line-per-file limit.

// TestSourceUpdateFetchesBeforeComparing is the regression test for the
// bug where a source update never fetched: it read the stale local @{u}
// left behind by whatever last touched the checkout, so a real upstream
// change could stay invisible to both --check and a real run forever. Both
// paths must run `git fetch` before deciding anything.
func TestSourceUpdateFetchesBeforeComparing(t *testing.T) {
	checkout := t.TempDir()
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	sameCommit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	runner := &fakeSourceRunner{
		checkout: checkout,
		git: &sourceGitState{
			branch:         "main",
			localCommit:    sameCommit,
			upstreamCommit: sameCommit,
			upstreamKnown:  true,
		},
		make: sourceMakeBehavior{reportedVersion: "0.0.0-dev"},
	}
	options := sourceUpdateOptions(t, installPath, checkout, runner)
	options.CheckOnly = true

	if _, err := Run(context.Background(), options); err != nil {
		t.Fatalf("Run() source check error = %v", err)
	}
	if !runner.ran("git fetch") {
		t.Fatalf("expected --check to run git fetch before comparing, calls = %v", runner.calls)
	}

	runner.calls = nil
	options.CheckOnly = false
	if _, err := Run(context.Background(), options); err != nil {
		t.Fatalf("Run() source update error = %v", err)
	}
	if !runner.ran("git fetch") {
		t.Fatalf("expected a real run to run git fetch before comparing, calls = %v", runner.calls)
	}
}

// TestSourceUpdateFetchFailureDoesNotTrustStaleUpToDate covers the case
// where the fetch itself fails (offline, unreachable remote): a real run
// must not use the now-possibly-stale local/upstream comparison to take the
// "nothing to pull" shortcut — it must still attempt `git pull --ff-only`
// and let that be the final word, and it must warn that the comparison
// might be stale.
func TestSourceUpdateFetchFailureDoesNotTrustStaleUpToDate(t *testing.T) {
	checkout := t.TempDir()
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	sameCommit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	runner := &fakeSourceRunner{
		checkout: checkout,
		git: &sourceGitState{
			branch:         "main",
			fetchErr:       fmt.Errorf("ssh: connect to host origin port 22: network is unreachable"),
			localCommit:    sameCommit,
			upstreamCommit: sameCommit,
			upstreamKnown:  true,
		},
		make: sourceMakeBehavior{reportedVersion: "0.0.0-dev"},
	}

	result, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner))
	if err != nil {
		t.Fatalf("Run() source update error = %v", err)
	}
	if !runner.ran("git pull --ff-only") {
		t.Fatalf("expected pull attempt despite stale up-to-date data when fetch failed, calls = %v", runner.calls)
	}
	foundWarning := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "git fetch") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("expected a fetch-failure warning, got %#v", result.Warnings)
	}
}

// TestSourceUpdateNeverWritesGlobalInstallMetadata is the regression test
// for a source update clobbering a co-resident release install's
// install.json: DetectInstall recognizes a source install purely from
// IsDevelopmentVersion(currentVersion), so a source update has never needed
// to write install.json, and doing so overwrites the one global file a
// release install's own self-update depends on.
func TestSourceUpdateNeverWritesGlobalInstallMetadata(t *testing.T) {
	home := missingMoltnetHome(t)
	t.Setenv(moltnetHomeEnv, home)

	releasePath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if err := writeInstallMetadata(installMetadata{
		InstallMethod:     string(InstallMethodReleaseTarball),
		InstallPath:       releasePath,
		SelfUpdateAllowed: true,
		Version:           1,
	}); err != nil {
		t.Fatalf("seed release install metadata: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(home, "install.json"))
	if err != nil {
		t.Fatalf("read seeded install.json: %v", err)
	}

	checkout := t.TempDir()
	sourcePath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	runner := &fakeSourceRunner{
		checkout: checkout,
		git: &sourceGitState{
			branch:         "main",
			localCommit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			upstreamCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			upstreamKnown:  true,
		},
		make: sourceMakeBehavior{reportedVersion: "0.0.0-dev"},
	}
	options := Options{
		CommandRunner:  runner,
		CurrentVersion: "0.0.0-dev",
		Detector:       staticDetector{install: Install{Method: InstallMethodSource, Path: sourcePath}},
		LockPath:       sourcePath + ".test.update.lock",
		Platform:       Platform{OS: "linux", Arch: "amd64"},
		SourceCheckout: checkout,
	}

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() source update error = %v", err)
	}
	if !result.Updated {
		t.Fatalf("expected source update to succeed, got %#v", result)
	}

	after, err := os.ReadFile(filepath.Join(home, "install.json"))
	if err != nil {
		t.Fatalf("read install.json after source update: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("source update wrote to the shared install.json:\nbefore: %s\nafter:  %s", before, after)
	}

	releaseInstall, err := DefaultInstallDetector{}.DetectInstall(context.Background(), "v0.1.0")
	if err != nil {
		t.Fatalf("DetectInstall: %v", err)
	}
	if releaseInstall.Method != InstallMethodReleaseTarball || !releaseInstall.SelfUpdateAllowed {
		t.Fatalf("release install lost self-update eligibility after a coexisting source update: %#v", releaseInstall)
	}
}

func TestSourceUpdateWarnsVersionFlagUnsupported(t *testing.T) {
	checkout := t.TempDir()
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	runner := &fakeSourceRunner{
		checkout: checkout,
		git: &sourceGitState{
			branch:         "main",
			localCommit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			upstreamCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			upstreamKnown:  true,
		},
	}
	options := sourceUpdateOptions(t, installPath, checkout, runner)
	options.CheckOnly = true
	options.TargetVersion = "v9.9.9"

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() source check error = %v", err)
	}
	found := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "--version") && strings.Contains(warning, "not supported") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a --version-unsupported-for-source warning, got %#v", result.Warnings)
	}
}

// TestSourceUpdateRefusalReasonIsNotDuplicatedInWarnings covers the P3
// "refusals print the same sentence twice" cleanup: a real run's refusal
// text belongs in RefusalReason (main.go already echoes it to stderr as the
// returned error), not also queued into Warnings, which report.go prints
// unconditionally on stdout.
func TestSourceUpdateRefusalReasonIsNotDuplicatedInWarnings(t *testing.T) {
	checkout := t.TempDir()
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	runner := &fakeSourceRunner{
		checkout: checkout,
		git: &sourceGitState{
			branch:         "main",
			dirty:          true,
			localCommit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			upstreamCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			upstreamKnown:  true,
		},
	}

	result, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner))
	if err == nil {
		t.Fatal("expected dirty working tree refusal")
	}
	if strings.TrimSpace(result.RefusalReason) == "" {
		t.Fatalf("expected RefusalReason to be set, got %#v", result)
	}
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "uncommitted changes") {
			t.Fatalf("real-run refusal text duplicated into Warnings: %#v", result.Warnings)
		}
	}
}

// TestSourceUpdatePreflightsInstallDirWritability covers the P2-6 fix: an
// unwritable install directory must be refused before pull/build run at
// all, not only after a full pull+build inside replaceInstalledBinary.
func TestSourceUpdatePreflightsInstallDirWritability(t *testing.T) {
	installDir := t.TempDir()
	installPath := writeMoltnetScript(t, installDir, "0.0.0-dev")
	if err := os.Chmod(installDir, 0o500); err != nil {
		t.Fatalf("make install directory read-only: %v", err)
	}
	defer os.Chmod(installDir, 0o755)

	checkout := t.TempDir()
	runner := &fakeSourceRunner{
		checkout: checkout,
		git: &sourceGitState{
			branch:         "main",
			localCommit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			upstreamCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			upstreamKnown:  true,
		},
		make: sourceMakeBehavior{reportedVersion: "0.0.0-dev"},
	}

	// The update lock must live outside the now-read-only installDir, so
	// this test isolates the writability preflight from lock acquisition
	// (which also needs to create a file).
	options := sourceUpdateOptions(t, installPath, checkout, runner)
	options.LockPath = filepath.Join(t.TempDir(), "update.lock")

	_, err := Run(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("expected a writability refusal, got %v", err)
	}
	if runner.ran("git pull") || runner.ran("make") {
		t.Fatalf("expected the writability preflight to refuse before pull/build ran, calls = %v", runner.calls)
	}
}

// TestSourceUpdateRunsCommandsInStampedCheckout pins that every git/make
// call a source update makes runs with dir set to exactly the stamped
// checkout path — never the process's cwd or some other directory.
func TestSourceUpdateRunsCommandsInStampedCheckout(t *testing.T) {
	checkout := t.TempDir()
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	runner := &fakeSourceRunner{
		checkout: checkout,
		git: &sourceGitState{
			branch:         "main",
			localCommit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			upstreamCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			upstreamKnown:  true,
		},
		make: sourceMakeBehavior{reportedVersion: "0.0.0-dev"},
	}

	if _, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner)); err != nil {
		t.Fatalf("Run() source update error = %v", err)
	}
	if len(runner.dirs) == 0 {
		t.Fatal("expected at least one command to run")
	}
	for index, dir := range runner.dirs {
		if dir != checkout {
			t.Fatalf("call %d (%s) ran in %q, want checkout %q", index, runner.calls[index], dir, checkout)
		}
	}
}
