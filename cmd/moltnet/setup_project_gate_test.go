package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestSetupProjectGitCheckoutGateRefusesNestedSubdirectory is the ALSO
// cross-unit item's core case: `moltnet setup --project` (or Q1 = "in this
// folder") from a subdirectory of an existing git checkout must refuse,
// since prefixedGitignoreCredentialEntries (init_checkout.go) still cannot
// correctly anchor its entries for a relative --dir target in that
// combination — verified empirically (filepath.Rel(absoluteGitRoot, ".")
// always errors on the abs-vs-relative mismatch, so the prefix silently
// stays empty).
func TestSetupProjectGitCheckoutGateRefusesNestedSubdirectory(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	sub := filepath.Join(repoRoot, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Chdir(sub)

	err := checkSetupProjectGitCheckoutGate()
	if !errors.Is(err, errSetupProjectNestedGitCheckout) {
		t.Fatalf("checkSetupProjectGitCheckoutGate() = %v, want errSetupProjectNestedGitCheckout", err)
	}
}

// TestSetupProjectGitCheckoutGateAllowsCheckoutRootItself: the one case
// where the same filepath.Rel mismatch happens to produce the *correct*
// (empty) prefix already — cwd already is the checkout root — must not be
// refused.
func TestSetupProjectGitCheckoutGateAllowsCheckoutRootItself(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Chdir(repoRoot)

	if err := checkSetupProjectGitCheckoutGate(); err != nil {
		t.Fatalf("checkSetupProjectGitCheckoutGate() = %v, want nil when cwd is already the checkout root", err)
	}
}

// TestSetupProjectGitCheckoutGateAllowsNoCheckoutAtAll: the common case (no
// git checkout anywhere in cwd's ancestry) is never affected by the
// underlying gap at all — gitCheckoutRoot itself returns ok=false, and
// offerGitignoreCredentialEntries/prefixedGitignoreCredentialEntries are
// never even reached (init.go's own `if gitRoot, ok := gitCheckoutRoot(root);
// ok` guard).
func TestSetupProjectGitCheckoutGateAllowsNoCheckoutAtAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(dir)

	if err := checkSetupProjectGitCheckoutGate(); err != nil {
		t.Fatalf("checkSetupProjectGitCheckoutGate() = %v, want nil outside any git checkout", err)
	}
}

// TestSetupInteractiveProjectScopeRefusesInsideNestedGitCheckout wires the
// gate into the real Q1 answer: choosing "in this folder" from inside a
// nested git checkout subdirectory must refuse before Q2 ever prompts for
// a network name.
func TestSetupInteractiveProjectScopeRefusesInsideNestedGitCheckout(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	sub := filepath.Join(repoRoot, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Chdir(sub)
	withTestDefaultPort(t)
	// withInProcessRunChild is load-bearing, not incidental: this test
	// currently passes because the gate refuses before runSetupInteractive
	// ever reaches setupPlanInvocations/execChild. If the gate is ever
	// mutated away, execChild would re-exec this very `go test` binary as
	// "moltnet init --dir .", which recurses (the child hits `go test`'s own
	// argv, not "init"'s) into a fork bomb instead of failing this test —
	// see setup_runner_test.go's withInProcessRunChild doc comment. Routing
	// through runChildInProcess makes a gate regression fail fast instead.
	withInProcessRunChild(t)

	withPromptAnswers(t, "2") // Q1 = "2": in this folder.
	var runErr error
	captureSetupOutput(t, func() {
		runErr = run(context.Background(), []string{"setup"}, "test")
	})
	if !errors.Is(runErr, errSetupProjectNestedGitCheckout) {
		t.Fatalf("run() error = %v, want it to wrap errSetupProjectNestedGitCheckout", runErr)
	}
}
