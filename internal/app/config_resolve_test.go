package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPathExplicitWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	writeResolveFixture(t, path)

	got, found, err := ResolveConfigPath(path, "")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != path {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true)", got, found, path)
	}
}

func TestResolveConfigPathFindsSoleHomeNetwork(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	networkPath := filepath.Join(home, ".moltnet", "acme", DefaultPath)
	writeResolveFixture(t, networkPath)

	got, found, err := ResolveConfigPath("", "")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != networkPath {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true)", got, found, networkPath)
	}
}

func TestResolveConfigPathDisambiguatesWithID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	writeResolveFixture(t, filepath.Join(home, ".moltnet", "acme", DefaultPath))
	writeResolveFixture(t, filepath.Join(home, ".moltnet", "beta", DefaultPath))

	got, found, err := ResolveConfigPath("", "beta")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	want := filepath.Join(home, ".moltnet", "beta", DefaultPath)
	if !found || got != want {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true)", got, found, want)
	}
}

func TestResolveConfigPathAmbiguousWithoutIDErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	writeResolveFixture(t, filepath.Join(home, ".moltnet", "acme", DefaultPath))
	writeResolveFixture(t, filepath.Join(home, ".moltnet", "beta", DefaultPath))

	_, _, err := ResolveConfigPath("", "")
	if err == nil {
		t.Fatal("expected an error for multiple networks without --id")
	}
}

func TestResolveConfigPathUnknownIDErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	writeResolveFixture(t, filepath.Join(home, ".moltnet", "acme", DefaultPath))

	if _, _, err := ResolveConfigPath("", "nonexistent"); err == nil {
		t.Fatal("expected an error for an unknown --id")
	}
}

func TestResolveConfigPathNotFoundIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	path, found, err := ResolveConfigPath("", "")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if found || path != "" {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (\"\", false)", path, found)
	}
}

func TestResolveConfigPathCwdWinsOverHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeResolveFixture(t, filepath.Join(home, ".moltnet", "acme", DefaultPath))

	cwd := t.TempDir()
	t.Chdir(cwd)
	cwdConfig := filepath.Join(cwd, DefaultPath)
	writeResolveFixture(t, cwdConfig)

	got, found, err := ResolveConfigPath("", "")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != DefaultPath {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true)", got, found, DefaultPath)
	}
}

func TestResolveNodeConfigPathFindsSoleHomeNetwork(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	networkPath := filepath.Join(home, ".moltnet", "acme", "MoltnetNode")
	writeResolveFixture(t, networkPath)

	got, found, err := ResolveNodeConfigPath("", "")
	if err != nil {
		t.Fatalf("ResolveNodeConfigPath() error = %v", err)
	}
	if !found || got != networkPath {
		t.Fatalf("ResolveNodeConfigPath() = (%q, %v), want (%q, true)", got, found, networkPath)
	}
}

func TestNetworkHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := NetworkHomeDir("acme")
	if err != nil {
		t.Fatalf("NetworkHomeDir() error = %v", err)
	}
	want := filepath.Join(home, ".moltnet", "acme")
	if got != want {
		t.Fatalf("NetworkHomeDir() = %q, want %q", got, want)
	}
}

func writeResolveFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
