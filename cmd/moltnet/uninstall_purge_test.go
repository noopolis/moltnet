package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunUninstallCommandPurgeSymlinkedHomeReportsLinkRemoved is the P2-2
// regression test: when ~/.moltnet is a symlink to data stored elsewhere,
// --purge must remove only the link, name the resolved target in its
// outcome message, and leave the data at that target untouched — never
// silently RemoveAll the target, and never claim "removed ~/.moltnet" as
// if the data itself was gone.
func TestRunUninstallCommandPurgeSymlinkedHomeReportsLinkRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	withScratchExecutable(t)

	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}
	root := filepath.Join(home, ".moltnet")
	if err := os.Symlink(target, root); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", target, err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes", "--purge"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	want := root + " was a symlink to " + resolvedTarget + "; removed the link, data at " + resolvedTarget + " untouched"
	if !strings.Contains(output, want) {
		t.Fatalf("output = %q, want it to contain %q", output, want)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("expected symlink %q to be removed, lstat err = %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(target, "acme")); err != nil {
		t.Fatalf("expected data at the symlink target to survive: %v", err)
	}
}

// TestRunUninstallCommandPurgeMissingHomeDirSaysDidNotExist is the P3
// regression test: --purge must not print "removed ~/.moltnet" when there
// was never anything there to remove.
func TestRunUninstallCommandPurgeMissingHomeDirSaysDidNotExist(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	withScratchExecutable(t)

	root := filepath.Join(home, ".moltnet")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes", "--purge"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	want := root + " did not exist"
	if !strings.Contains(output, want) {
		t.Fatalf("output = %q, want it to contain %q", output, want)
	}
	if strings.Contains(output, "removed "+root) {
		t.Fatalf("output = %q, want it to not falsely claim %q was removed", output, root)
	}
}

// TestRunUninstallCommandPurgeRemovesMoltnetHomeOverride is the P2-4
// regression test: when MOLTNET_HOME points updater's install state
// somewhere other than the default ~/.moltnet, --purge must remove that
// location too and say so, instead of leaving it behind untouched and
// unmentioned.
func TestRunUninstallCommandPurgeRemovesMoltnetHomeOverride(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	withScratchExecutable(t)

	overrideHome := t.TempDir()
	t.Setenv("MOLTNET_HOME", overrideHome)
	if err := os.WriteFile(filepath.Join(overrideHome, "install.json"), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("write install.json: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes", "--purge"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "removed "+overrideHome) {
		t.Fatalf("output = %q, want it to report removing the MOLTNET_HOME override %q", output, overrideHome)
	}
	if _, err := os.Stat(overrideHome); !os.IsNotExist(err) {
		t.Fatalf("expected MOLTNET_HOME override %q to be removed, stat err = %v", overrideHome, err)
	}
}

// TestRunUninstallCommandPlanMentionsMoltnetHomeOverrideWithoutPurge is the
// P2-4 regression test for the non-purge path: MOLTNET_HOME state survives
// a plain (non-purge) uninstall just like ~/.moltnet does, but the plan
// must say so explicitly rather than staying silent about it.
func TestRunUninstallCommandPlanMentionsMoltnetHomeOverrideWithoutPurge(t *testing.T) {
	withFakeServiceManager(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	withScratchExecutable(t)

	overrideHome := t.TempDir()
	t.Setenv("MOLTNET_HOME", overrideHome)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"uninstall", "--yes"}, "test"); err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
	})

	if !strings.Contains(output, "MOLTNET_HOME is set to "+overrideHome) {
		t.Fatalf("output = %q, want it to mention the MOLTNET_HOME override", output)
	}
	if _, err := os.Stat(overrideHome); err != nil {
		t.Fatalf("expected MOLTNET_HOME override %q to survive without --purge: %v", overrideHome, err)
	}
}
