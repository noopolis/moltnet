package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file cover checkoutWarning, gitCheckoutRoot, and
// offerGitignoreCredentialEntries (init_checkout.go) -- split out of
// init_home_test.go to keep that file under the repo's 400-line limit.

func TestRunInitWarnsOnCheckoutMarkers(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if !strings.Contains(output, "warning") || !strings.Contains(output, "go.mod") {
		t.Fatalf("expected a checkout warning mentioning go.mod, got %q", output)
	}
}

func TestCheckoutWarningEmptyForCleanDirectory(t *testing.T) {
	directory := t.TempDir()
	if warning := checkoutWarning(directory); warning != "" {
		t.Fatalf("checkoutWarning() = %q, want empty", warning)
	}
}

// TestCheckoutWarningNamesAdminCredential pins P1-3's escalated fix (a
// second, independent review rated this P0): the warning must explicitly
// say Moltnet will hold this network's admin credential, not just that
// writing a config here is "unusual." A regression that reworded this back
// to the pre-fix "unusual for a runtime install" phrasing, with no mention
// of a credential at all, must fail here -- not just leave a generic
// "warning" substring check satisfied, which the pre-fix wording already
// passed.
func TestCheckoutWarningNamesAdminCredential(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	warning := checkoutWarning(directory)
	if !strings.Contains(warning, "admin credential") {
		t.Fatalf("expected checkoutWarning to name the admin credential explicitly, got %q", warning)
	}
}

// TestRunInitOffersGitignoreForGitCheckout pins the other half of PLAN.md
// 7.0's "Corollary work, same change": `init --dir` into an actual git
// checkout (a ".git" marker specifically, not just go.mod/package.json)
// appends Moltnet, MoltnetNode, and .moltnet/ to .gitignore, creating it if
// absent, and reports having done so. A rerun against the same directory
// must be idempotent -- no duplicate lines, and nothing new to report.
func TestRunInitOffersGitignoreForGitCheckout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	if !strings.Contains(output, "added Moltnet, MoltnetNode, .moltnet/, .moltnet-init-*.tmp to .gitignore") {
		t.Fatalf("expected a note reporting the .gitignore entries added, got %q", output)
	}

	gitignorePath := filepath.Join(directory, ".gitignore")
	contents, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, entry := range []string{"Moltnet", "MoltnetNode", ".moltnet/", ".moltnet-init-*.tmp"} {
		if !strings.Contains(string(contents), entry) {
			t.Fatalf("expected .gitignore to contain %q, got:\n%s", entry, contents)
		}
	}

	// Rerun: every entry is already covered, so nothing new should be added
	// or reported, and the file must not grow duplicate lines.
	rerunOutput := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() rerun error = %v", err)
		}
	})
	if strings.Contains(rerunOutput, ".gitignore") {
		t.Fatalf("expected the idempotent rerun to say nothing about .gitignore, got %q", rerunOutput)
	}
	rerunContents, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore after rerun: %v", err)
	}
	if string(rerunContents) != string(contents) {
		t.Fatalf(".gitignore changed on an idempotent rerun:\nbefore:\n%s\nafter:\n%s", contents, rerunContents)
	}
}

// TestOfferGitignoreCredentialEntriesPreservesExistingContent covers the
// non-empty-.gitignore case directly: an operator's own pre-existing
// .gitignore, with its own unrelated entries and no trailing newline, must
// keep every existing line untouched and get exactly the missing entries
// appended -- never a wholesale rewrite.
func TestOfferGitignoreCredentialEntriesPreservesExistingContent(t *testing.T) {
	directory := t.TempDir()
	gitignorePath := filepath.Join(directory, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("node_modules/\nMoltnet"), 0o644); err != nil {
		t.Fatalf("write fixture .gitignore: %v", err)
	}

	added, err := offerGitignoreCredentialEntries(directory, directory)
	if err != nil {
		t.Fatalf("offerGitignoreCredentialEntries() error = %v", err)
	}
	if len(added) != 3 || added[0] != "MoltnetNode" || added[1] != ".moltnet/" || added[2] != ".moltnet-init-*.tmp" {
		t.Fatalf("expected exactly [MoltnetNode .moltnet/ .moltnet-init-*.tmp] added (Moltnet already present), got %v", added)
	}

	contents, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(contents), "node_modules/") {
		t.Fatalf("expected the operator's own existing entry to survive, got:\n%s", contents)
	}

	second, err := offerGitignoreCredentialEntries(directory, directory)
	if err != nil {
		t.Fatalf("offerGitignoreCredentialEntries() rerun error = %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("expected a rerun to add nothing once every entry is covered, got %v", second)
	}
}

// TestRunInitOffersGitignoreForGitCheckoutSubdirectory is round 2's P1 fix:
// `init --dir` into a directory that has no marker of its own but sits
// inside an ancestor git checkout (".git" only at the checkout's own root,
// not under the --dir target) must still warn -- naming the discovered
// checkout root, since the target itself has nothing to point at -- and
// must still write .gitignore entries, into that discovered root, prefixed
// with the target's path relative to it (e.g. "agents/Moltnet") so the rule
// actually matches where the files land. Before this fix, both the marker
// loop and the git-detection helper only ever looked directly under root,
// so this exact shape got no warning and no .gitignore entry at all --
// strictly worse than the pre-fix root-is-a-checkout case, which at least
// warned generically.
func TestRunInitOffersGitignoreForGitCheckoutSubdirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	target := filepath.Join(repoRoot, "agents")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", target}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if !strings.Contains(output, "warning") || !strings.Contains(output, repoRoot) {
		t.Fatalf("expected a warning naming the discovered checkout root %q, got %q", repoRoot, output)
	}
	if !strings.Contains(output, "added agents/Moltnet, agents/MoltnetNode, agents/.moltnet/, agents/.moltnet-init-*.tmp to .gitignore") {
		t.Fatalf("expected a note reporting the path-prefixed .gitignore entries added, got %q", output)
	}

	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	contents, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, entry := range []string{"agents/Moltnet", "agents/MoltnetNode", "agents/.moltnet/", "agents/.moltnet-init-*.tmp"} {
		if !strings.Contains(string(contents), entry) {
			t.Fatalf("expected .gitignore to contain %q, got:\n%s", entry, contents)
		}
	}

	// git status equivalent: the .gitignore lives at the checkout root, not
	// under target -- confirm nothing was written into target itself.
	if _, err := os.Stat(filepath.Join(target, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("expected no .gitignore written under the --dir target itself, stat err = %v", err)
	}
}

// TestGitCheckoutRootStopsClimbingAboveHome pins gitCheckoutRoot's $HOME
// bound directly: a ".git" that only exists above $HOME (enclosing it) must
// never be found, however deep the target sits under $HOME -- the walk
// checks $HOME itself and then stops, rather than climbing to $HOME's
// parent looking further.
func TestGitCheckoutRootStopsClimbingAboveHome(t *testing.T) {
	above := t.TempDir()
	if err := os.Mkdir(filepath.Join(above, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	home := filepath.Join(above, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	target := filepath.Join(home, "project", "agents")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	if root, ok := gitCheckoutRoot(target); ok {
		t.Fatalf("gitCheckoutRoot() = (%q, true), want ok=false -- a repo enclosing $HOME must not be found", root)
	}
}

// TestGitCheckoutRootChecksHomeItself is the other half of the same bound:
// $HOME is the last directory checked, not the first one excluded -- a
// checkout rooted exactly at $HOME is still correctly reported for anything
// nested under it.
func TestGitCheckoutRootChecksHomeItself(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	t.Setenv("HOME", home)

	target := filepath.Join(home, "project")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	root, ok := gitCheckoutRoot(target)
	if !ok || root != home {
		t.Fatalf("gitCheckoutRoot() = (%q, %v), want (%q, true)", root, ok, home)
	}
}

// TestWriteFileIfMissingRefusesSymlinkTarget, TestWriteFileIfMissingRefusesDanglingSymlink,
// and TestWriteFileIfMissingNoOpNeverCreatesTempFile live in
// init_checkout_writefileifmissing_test.go -- split out to keep this file
// under the repo's 400-line limit.
