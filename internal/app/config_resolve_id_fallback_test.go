package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveConfigPathCwdBoundaryRuneResolvesCwdNotGlobal is the P0-1
// resolution-level regression the reviewer live-confirmed: a valid cwd
// config whose 512th byte splits a multibyte rune (€) must still win over a
// sole global network under ~/.moltnet — not get misclassified as binary,
// skipped with a warning, and silently resolve the wrong (global) network.
func TestResolveConfigPathCwdBoundaryRuneResolvesCwdNotGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	cwdConfig := filepath.Join(cwd, DefaultPath)
	if err := os.WriteFile(cwdConfig, boundaryRuneConfigContents("cwd-net"), 0o600); err != nil {
		t.Fatalf("write cwd fixture: %v", err)
	}

	writeResolveFixture(t, filepath.Join(home, ".moltnet", "global-net", DefaultPath))

	var warnings bytes.Buffer
	original := configWarnWriter
	configWarnWriter = &warnings
	defer func() { configWarnWriter = original }()

	got, found, err := ResolveConfigPath("", "")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != DefaultPath {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true) — the cwd config, not the global network", got, found, DefaultPath)
	}
	if warnings.Len() != 0 {
		t.Fatalf("expected no binary-shadow warning, got %q", warnings.String())
	}
}

// TestResolveConfigPathIDFallsBackToNameMatchedCwd is the P0-2 field
// scenario: a `moltnet init --dir` install has no ~/.moltnet/<id>/, so
// --id must resolve it by reading the cwd config's own network.id — never
// by cwd precedence, only by name match.
func TestResolveConfigPathIDFallsBackToNameMatchedCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeNetworkIDFixture(t, filepath.Join(cwd, DefaultPath), "acme")

	got, found, err := ResolveConfigPath("", "acme")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != DefaultPath {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true)", got, found, DefaultPath)
	}
}

// TestResolveConfigPathIDMismatchedCwdErrorsClearly covers the other half
// of P0-2: a cwd config that does not declare the requested id must never
// be silently accepted in its place.
func TestResolveConfigPathIDMismatchedCwdErrorsClearly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeNetworkIDFixture(t, filepath.Join(cwd, DefaultPath), "acme")

	_, found, err := ResolveConfigPath("", "bob")
	if found {
		t.Fatal("expected found=false for a mismatched cwd network id")
	}
	if err == nil {
		t.Fatal("expected a clear error, not a silent resolution of the mismatched cwd config")
	}
	if !strings.Contains(err.Error(), "bob") {
		t.Fatalf("expected the error to name the requested id, got %v", err)
	}
}

// TestResolveConfigPathIDPrefersGlobalOverNameMatchedCwd: when
// ~/.moltnet/<id>/ does exist, it still wins outright — the cwd fallback
// only ever engages when the global network is entirely absent.
func TestResolveConfigPathIDPrefersGlobalOverNameMatchedCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeNetworkIDFixture(t, filepath.Join(cwd, DefaultPath), "acme")

	globalPath := filepath.Join(home, ".moltnet", "acme", DefaultPath)
	writeResolveFixture(t, globalPath)

	got, found, err := ResolveConfigPath("", "acme")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != globalPath {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true) — the global network, not the cwd fallback", got, found, globalPath)
	}
}

// TestResolveConfigPathUnreadableSiblingNetworkIsSkipped is the P2-3
// regression: a sniff read error on one sibling network directory (a
// permission-denied config file, standing in for anything that makes
// os.Open fail besides plain absence) must not break no-flag/no-id
// resolution for every other network under ~/.moltnet — that candidate is
// just skipped, like any other unusable one.
func TestResolveConfigPathUnreadableSiblingNetworkIsSkipped(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores unix file permissions")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	unreadable := filepath.Join(home, ".moltnet", "locked", DefaultPath)
	writeResolveFixture(t, unreadable)
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(unreadable, 0o600)

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

// TestResolveNodeConfigPathIDFallsBackToNameMatchedCwd is
// TestResolveConfigPathIDFallsBackToNameMatchedCwd's `moltnet node`
// counterpart (P0-2): a --dir install's MoltnetNode has no
// ~/.moltnet/<id>/, so --id must resolve it by reading the cwd config's own
// moltnet.network_id.
func TestResolveNodeConfigPathIDFallsBackToNameMatchedCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeNodeNetworkIDFixture(t, filepath.Join(cwd, "MoltnetNode"), "acme")

	got, found, err := ResolveNodeConfigPath("", "acme")
	if err != nil {
		t.Fatalf("ResolveNodeConfigPath() error = %v", err)
	}
	if !found || got != "MoltnetNode" {
		t.Fatalf("ResolveNodeConfigPath() = (%q, %v), want (%q, true)", got, found, "MoltnetNode")
	}
}

// TestResolveNodeConfigPathIDMismatchedCwdErrorsClearly is
// TestResolveConfigPathIDMismatchedCwdErrorsClearly's `moltnet node`
// counterpart: a cwd MoltnetNode that declares a different network id must
// never be silently accepted in the requested id's place.
func TestResolveNodeConfigPathIDMismatchedCwdErrorsClearly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeNodeNetworkIDFixture(t, filepath.Join(cwd, "MoltnetNode"), "acme")

	_, found, err := ResolveNodeConfigPath("", "bob")
	if found {
		t.Fatal("expected found=false for a mismatched cwd network id")
	}
	if err == nil {
		t.Fatal("expected a clear error, not a silent resolution of the mismatched cwd config")
	}
	if !strings.Contains(err.Error(), "bob") {
		t.Fatalf("expected the error to name the requested id, got %v", err)
	}
}

// writeNetworkIDFixture writes a minimal but valid Moltnet config declaring
// network.id — used by the P0-2 name-matched cwd fallback tests, which need
// a real parseable id, unlike writeResolveFixture's opaque placeholder.
func writeNetworkIDFixture(t *testing.T, path string, networkID string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	contents := "version: moltnet.v1\nnetwork:\n  id: " + networkID + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// writeNodeNetworkIDFixture is writeNetworkIDFixture's MoltnetNode
// counterpart, declaring moltnet.network_id.
func writeNodeNetworkIDFixture(t *testing.T, path string, networkID string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	contents := "version: moltnet.node.v1\nmoltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: " + networkID + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
