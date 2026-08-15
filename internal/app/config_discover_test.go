package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPathSupportsExplicitAndEnvPaths(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	explicit := filepath.Join(directory, "custom.yaml")
	writeConfigFile(t, explicit, `
version: moltnet.v1
network:
  id: explicit
`)

	path, ok, err := DiscoverPath(explicit)
	if err != nil {
		t.Fatalf("DiscoverPath(explicit) error = %v", err)
	}
	if !ok || path != explicit {
		t.Fatalf("unexpected explicit discovery result ok=%v path=%q", ok, path)
	}

	t.Setenv("MOLTNET_CONFIG", explicit)
	path, ok, err = DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath(env) error = %v", err)
	}
	if !ok || path != explicit {
		t.Fatalf("unexpected env discovery result ok=%v path=%q", ok, path)
	}
}

func TestDiscoverPathFindsFallbackCandidates(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	writeConfigFile(t, filepath.Join(directory, "moltnet.json"), `{
  "version": "moltnet.v1",
  "network": { "id": "json-net" }
}`)

	path, ok, err := DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath() error = %v", err)
	}
	if !ok || path != "moltnet.json" {
		t.Fatalf("unexpected discovered fallback ok=%v path=%q", ok, path)
	}
}

func TestDiscoverPathRejectsMissingOrDirectoryPaths(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	if _, _, err := DiscoverPath(filepath.Join(directory, "missing.yaml")); err == nil {
		t.Fatal("expected missing explicit path error")
	}

	if _, _, err := DiscoverPath(directory); err == nil {
		t.Fatal("expected directory explicit path error")
	}
}

func chdirForTest(t *testing.T, directory string) func() {
	t.Helper()

	t.Chdir(directory)
	return func() {}
}

func writeConfigFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config file %q: %v", path, err)
	}
}
