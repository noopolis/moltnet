package updater

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The fakeSourceRunner harness these tests drive (fakeSourceRunner,
// sourceGitState, sourceMakeBehavior, sourceUpdateOptions) lives in
// sourceupdate_fake_runner_test.go.

func TestSourceUpdateCleanTreeHappyPath(t *testing.T) {
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

	result, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner))
	if err != nil {
		t.Fatalf("Run() source update error = %v", err)
	}
	if !result.Updated || result.BackupPath == "" {
		t.Fatalf("expected source update to succeed, got %#v", result)
	}
	if got := runVersion(t, installPath); got != "0.0.0-dev" {
		t.Fatalf("installed version = %q", got)
	}
	if got := runVersion(t, result.BackupPath); got != "0.0.0-dev" {
		t.Fatalf("backup version = %q", got)
	}
	if result.SourceUpdate == nil || len(result.SourceUpdate.Steps) == 0 {
		t.Fatalf("expected recorded source update steps, got %#v", result.SourceUpdate)
	}
	if !runner.ran("git pull --ff-only") || !runner.ran("make build") {
		t.Fatalf("expected pull and build to run, got calls %v", runner.calls)
	}

	info, err := os.Stat(installPath)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("installed binary lost its executable bit: %o", info.Mode().Perm())
	}
}

func TestSourceUpdateRefusesDirtyWorkingTree(t *testing.T) {
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
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("unexpected error %v", err)
	}
	if !result.MutationRefused {
		t.Fatalf("expected MutationRefused, got %#v", result)
	}
	if got := runVersion(t, installPath); got != "0.0.0-dev" {
		t.Fatalf("dirty tree refusal mutated install, version = %q", got)
	}
	if runner.ran("git pull") || runner.ran("make") {
		t.Fatalf("dirty tree refusal ran a mutating command, calls = %v", runner.calls)
	}
}

func TestSourceUpdateRefusesDetachedHead(t *testing.T) {
	checkout := t.TempDir()
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	runner := &fakeSourceRunner{
		checkout: checkout,
		git: &sourceGitState{
			detached:       true,
			localCommit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			upstreamCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			upstreamKnown:  true,
		},
	}

	result, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner))
	if err == nil {
		t.Fatal("expected detached HEAD refusal")
	}
	if !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("unexpected error %v", err)
	}
	if !result.MutationRefused {
		t.Fatalf("expected MutationRefused, got %#v", result)
	}
	if got := runVersion(t, installPath); got != "0.0.0-dev" {
		t.Fatalf("detached HEAD refusal mutated install, version = %q", got)
	}
	if runner.ran("git pull") || runner.ran("make") {
		t.Fatalf("detached HEAD refusal ran a mutating command, calls = %v", runner.calls)
	}
}

func TestSourceUpdateBuildFailureLeavesOldBinaryIntact(t *testing.T) {
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
		make: sourceMakeBehavior{err: fmt.Errorf("build failed: compile error in cmd/moltnet")},
	}

	result, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner))
	if err == nil || !strings.Contains(err.Error(), "installed binary left unchanged") {
		t.Fatalf("expected build failure to leave binary unchanged, got %v", err)
	}
	if result.Updated {
		t.Fatalf("build failure reported Updated, got %#v", result)
	}
	if got := runVersion(t, installPath); got != "0.0.0-dev" {
		t.Fatalf("build failure mutated install, version = %q", got)
	}
	if !runner.ran("git pull --ff-only") {
		t.Fatalf("expected pull to run before the build failure, calls = %v", runner.calls)
	}
}

func TestSourceUpdateVersionVerifyFailureLeavesOldBinaryIntact(t *testing.T) {
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
		make: sourceMakeBehavior{}, // reportedVersion empty: built binary fails to run
	}

	result, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner))
	if err == nil || !strings.Contains(err.Error(), "installed binary left unchanged") {
		t.Fatalf("expected version-verify failure to leave binary unchanged, got %v", err)
	}
	if result.Updated {
		t.Fatalf("version-verify failure reported Updated, got %#v", result)
	}
	if got := runVersion(t, installPath); got != "0.0.0-dev" {
		t.Fatalf("version-verify failure mutated install, version = %q", got)
	}
}

func TestSourceUpdateCheckReportsPlanWithoutMutating(t *testing.T) {
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
	options := sourceUpdateOptions(t, installPath, checkout, runner)
	options.CheckOnly = true

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() source check error = %v", err)
	}
	if result.SourceUpdate == nil || !result.SourceUpdate.RebuildNeeded {
		t.Fatalf("expected rebuild-needed plan, got %#v", result.SourceUpdate)
	}
	if !result.UpdateAvailable {
		t.Fatalf("expected UpdateAvailable, got %#v", result)
	}
	if got := runVersion(t, installPath); got != "0.0.0-dev" {
		t.Fatalf("check mutated install, version = %q", got)
	}
	if runner.ran("git pull") || runner.ran("make") {
		t.Fatalf("check ran a mutating command, calls = %v", runner.calls)
	}

	options.CheckOnly = false
	options.DryRun = true
	dryRunResult, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() source dry-run error = %v", err)
	}
	if dryRunResult.SourceUpdate == nil || !dryRunResult.SourceUpdate.RebuildNeeded {
		t.Fatalf("expected rebuild-needed dry-run plan, got %#v", dryRunResult.SourceUpdate)
	}
	if got := runVersion(t, installPath); got != "0.0.0-dev" {
		t.Fatalf("dry-run mutated install, version = %q", got)
	}
	if runner.ran("git pull") || runner.ran("make") {
		t.Fatalf("dry-run ran a mutating command, calls = %v", runner.calls)
	}
}

func TestSourceUpdateNoOpWhenAlreadyUpToDate(t *testing.T) {
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
	}

	result, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner))
	if err != nil {
		t.Fatalf("Run() source no-op error = %v", err)
	}
	if result.Updated || result.UpdateAvailable {
		t.Fatalf("expected no-op result, got %#v", result)
	}
	if runner.ran("git pull") || runner.ran("make") {
		t.Fatalf("up-to-date checkout ran a mutating command, calls = %v", runner.calls)
	}
}

func TestSourceUpdateWithoutStampedCheckoutDegradesToPlainRefusal(t *testing.T) {
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	options := sourceUpdateOptions(t, installPath, "", nil)

	result, err := Run(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "source or development builds") {
		t.Fatalf("expected plain source refusal, got err=%v result=%#v", err, result)
	}
	if !result.MutationRefused {
		t.Fatalf("expected MutationRefused, got %#v", result)
	}
}

func TestSourceUpdateCheckWithoutStampedCheckoutWarnsWithoutError(t *testing.T) {
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	options := sourceUpdateOptions(t, installPath, "", nil)
	options.CheckOnly = true

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() unstamped source check error = %v", err)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "source or development builds") {
		t.Fatalf("expected plain source refusal warning, got %#v", result.Warnings)
	}
	if result.MutationRefused {
		t.Fatalf("check should not set MutationRefused, got %#v", result)
	}
}

func TestSourceUpdateRefusesWhenStampedCheckoutIsNotAGitRepo(t *testing.T) {
	checkout := t.TempDir()
	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	runner := &fakeSourceRunner{checkout: checkout, git: &sourceGitState{notARepo: true}}

	result, err := Run(context.Background(), sourceUpdateOptions(t, installPath, checkout, runner))
	if err == nil || !strings.Contains(err.Error(), "source or development builds") {
		t.Fatalf("expected plain source refusal, got %v", err)
	}
	if !result.MutationRefused {
		t.Fatalf("expected MutationRefused, got %#v", result)
	}
}

// The fix-round regression tests (fetch-before-compare, install-metadata
// isolation, refusal-reason dedup, the writability preflight, the
// stamped-checkout dir pin, the --version-on-source warning) live in
// sourceupdate_fixround_test.go, split out to stay under this package's
// 400-line-per-file limit.

// replaceInstalledBinary itself (writability refusal, executable-bit
// preservation, backup) is covered in replace_installed_binary_test.go.
