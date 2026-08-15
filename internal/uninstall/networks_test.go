package uninstall

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/internal/service"
)

type fakeRunner struct{}

func (fakeRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

func TestNetworkIDsUnionsHomeDirAndInstalledUnits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// "acme" has both a network directory and an installed unit; "beta" has
	// only a network directory (service never installed, or already
	// uninstalled by hand); "gamma" has only a dangling unit (its network
	// directory was already removed by hand).
	if err := os.MkdirAll(filepath.Join(home, ".moltnet", "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".moltnet", "beta"), 0o700); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}

	manager := service.NewForOS(fakeRunner{}, "linux")
	for _, id := range []string{"acme", "gamma"} {
		spec := service.Spec{
			NetworkID:  id,
			ConfigPath: filepath.Join(home, ".moltnet", id, "Moltnet"),
			BinaryPath: "/usr/local/bin/moltnet",
			NetworkDir: filepath.Join(home, ".moltnet", id),
		}
		if err := manager.Install(context.Background(), spec); err != nil {
			t.Fatalf("Install(%q) error = %v", id, err)
		}
	}
	// Remove gamma's network directory by hand, leaving only its unit file.
	if err := os.RemoveAll(filepath.Join(home, ".moltnet", "gamma")); err != nil {
		t.Fatalf("remove gamma network dir: %v", err)
	}

	ids, err := NetworkIDs(manager)
	if err != nil {
		t.Fatalf("NetworkIDs() error = %v", err)
	}
	want := []string{"acme", "beta", "gamma"}
	if len(ids) != len(want) {
		t.Fatalf("NetworkIDs() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("NetworkIDs() = %v, want %v", ids, want)
		}
	}
}

// TestNetworkIDsSkipsDotDirectories is the P3 regression test: a hidden
// directory under ~/.moltnet/ (e.g. a stray ".DS_Store" or an editor swap
// dir) is not a network `moltnet init` ever created, and must not show up
// in the uninstall plan or be handed to service.Manager as a network id.
func TestNetworkIDsSkipsDotDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".moltnet", "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".moltnet", ".DS_Store"), 0o700); err != nil {
		t.Fatalf("mkdir .DS_Store: %v", err)
	}

	manager := service.NewForOS(fakeRunner{}, "linux")
	ids, err := NetworkIDs(manager)
	if err != nil {
		t.Fatalf("NetworkIDs() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != "acme" {
		t.Fatalf("NetworkIDs() = %v, want [acme] (dot-directory excluded)", ids)
	}
}

// TestNetworkIDsSkipsIDsThatDoNotSurviveTrimSpace is the P3 regression
// test: an id with leading/trailing whitespace would build a different
// on-disk unit path than the id itself resolves to
// (service.SystemdUnitName trims), so "acme " and "acme" would silently
// become two different networks in the plan instead of one.
func TestNetworkIDsSkipsIDsThatDoNotSurviveTrimSpace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".moltnet", "acme "), 0o700); err != nil {
		t.Fatalf("mkdir \"acme \": %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".moltnet", "beta"), 0o700); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}

	manager := service.NewForOS(fakeRunner{}, "linux")
	ids, err := NetworkIDs(manager)
	if err != nil {
		t.Fatalf("NetworkIDs() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != "beta" {
		t.Fatalf("NetworkIDs() = %v, want [beta] (untrimmed \"acme \" excluded)", ids)
	}
}

func TestNetworkIDsEmptyWhenNothingInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	manager := service.NewForOS(fakeRunner{}, "linux")
	ids, err := NetworkIDs(manager)
	if err != nil {
		t.Fatalf("NetworkIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("NetworkIDs() = %v, want empty", ids)
	}
}

func TestNetworkIDsUnsupportedOSStillListsHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".moltnet", "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}

	manager := service.NewForOS(fakeRunner{}, "windows")
	ids, err := NetworkIDs(manager)
	if err != nil {
		t.Fatalf("NetworkIDs() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != "acme" {
		t.Fatalf("NetworkIDs() = %v, want [acme]", ids)
	}
}
