package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// TestResolveConfigPathIDIgnoresCwd covers the field-reported P1: --id must
// resolve only ~/.moltnet/<id>/, never cwd, even when a same-named file
// sits in the current directory (this fixture is plain text; the binary
// shadowing case is covered separately by TestResolveConfigPathIDWithBinaryCwdShadowUsesGlobal).
func TestResolveConfigPathIDIgnoresCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeResolveFixture(t, filepath.Join(cwd, DefaultPath))

	networkPath := filepath.Join(home, ".moltnet", "acme", DefaultPath)
	writeResolveFixture(t, networkPath)

	got, found, err := ResolveConfigPath("", "acme")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != networkPath {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true)", got, found, networkPath)
	}
}

// TestResolveConfigPathIDIgnoresCwdWhenGlobalAbsent is the "absent global"
// half of the same case: with --id given, a cwd Moltnet must never be used
// as a substitute even when ~/.moltnet/<id>/ does not exist — the error
// must name the missing global path, not silently fall back to cwd.
func TestResolveConfigPathIDIgnoresCwdWhenGlobalAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeResolveFixture(t, filepath.Join(cwd, DefaultPath))

	_, found, err := ResolveConfigPath("", "acme")
	if found {
		t.Fatal("expected found=false")
	}
	if err == nil {
		t.Fatal("expected an error naming the missing ~/.moltnet/acme network, not a silent cwd fallback")
	}
	if !strings.Contains(err.Error(), filepath.Join(home, ".moltnet")) {
		t.Fatalf("expected error to name %s, got %v", filepath.Join(home, ".moltnet"), err)
	}
}

// TestResolveConfigPathIDWithBinaryCwdShadowUsesGlobal reproduces the exact
// field scenario: a stray compiled `./moltnet` binary matches the config
// filename "Moltnet" on a case-insensitive filesystem, and `--id` is passed.
// Fix #1 alone (id bypasses cwd) already prevents the binary from ever being
// stat'd as a candidate here; this test guards the combination.
func TestResolveConfigPathIDWithBinaryCwdShadowUsesGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.WriteFile(filepath.Join(cwd, DefaultPath), binaryLikeCandidateContents(), 0o755); err != nil {
		t.Fatalf("write binary candidate: %v", err)
	}

	networkPath := filepath.Join(home, ".moltnet", "alice-net", DefaultPath)
	writeResolveFixture(t, networkPath)

	got, found, err := ResolveConfigPath("", "alice-net")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != networkPath {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true)", got, found, networkPath)
	}
}

// TestResolveConfigPathSkipsBinaryShadowedCwdFallsToHome covers the
// binary-content guard end to end (no --id): a binary-shadowed cwd
// candidate is skipped with a warning, and resolution falls through to the
// sole global network exactly as if the shadow were never there.
func TestResolveConfigPathSkipsBinaryShadowedCwdFallsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.WriteFile(filepath.Join(cwd, DefaultPath), binaryLikeCandidateContents(), 0o755); err != nil {
		t.Fatalf("write binary candidate: %v", err)
	}

	networkPath := filepath.Join(home, ".moltnet", "acme", DefaultPath)
	writeResolveFixture(t, networkPath)

	var warnings bytes.Buffer
	original := configWarnWriter
	configWarnWriter = &warnings
	defer func() { configWarnWriter = original }()

	got, found, err := ResolveConfigPath("", "")
	if err != nil {
		t.Fatalf("ResolveConfigPath() error = %v", err)
	}
	if !found || got != networkPath {
		t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, true)", got, found, networkPath)
	}
	if !strings.Contains(warnings.String(), "not a text config file") {
		t.Fatalf("expected a binary-shadow warning, got %q", warnings.String())
	}
}

// TestResolveConfigPathCwdBoundaryRuneResolvesCwdNotGlobal is the P0-1
// resolution-level regression the reviewer live-confirmed: a valid cwd
// config whose 512th byte splits a multibyte rune (€) must still win over a
// sole global network under ~/.moltnet — not get misclassified as binary,
// skipped with a warning, and silently resolve the wrong (global) network.
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

// TestResolveNodeConfigPathIDIgnoresCwd is ResolveConfigPathIDIgnoresCwd's
// counterpart for `moltnet node --id`.
func TestResolveNodeConfigPathIDIgnoresCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeResolveFixture(t, filepath.Join(cwd, "MoltnetNode"))

	networkPath := filepath.Join(home, ".moltnet", "acme", "MoltnetNode")
	writeResolveFixture(t, networkPath)

	got, found, err := ResolveNodeConfigPath("", "acme")
	if err != nil {
		t.Fatalf("ResolveNodeConfigPath() error = %v", err)
	}
	if !found || got != networkPath {
		t.Fatalf("ResolveNodeConfigPath() = (%q, %v), want (%q, true)", got, found, networkPath)
	}
}

// TestResolveNodeConfigPathIDFallsBackToNameMatchedCwd is
// TestResolveConfigPathIDFallsBackToNameMatchedCwd's `moltnet node`
// counterpart (P0-2): a --dir install's MoltnetNode has no
// ~/.moltnet/<id>/, so --id must resolve it by reading the cwd config's own
// moltnet.network_id.
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

func TestListNetworkIDsSortedAndDeduped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeResolveFixture(t, filepath.Join(home, ".moltnet", "beta", DefaultPath))
	// A network directory with no config file at all still counts: it is
	// enumerated purely by directory name, not by DiscoverPath's filename
	// filter.
	if err := os.MkdirAll(filepath.Join(home, ".moltnet", "acme"), 0o700); err != nil {
		t.Fatalf("mkdir acme: %v", err)
	}

	ids, err := ListNetworkIDs()
	if err != nil {
		t.Fatalf("ListNetworkIDs() error = %v", err)
	}
	want := []string{"acme", "beta"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("ListNetworkIDs() = %v, want %v", ids, want)
	}
}

func TestListNetworkIDsMissingHomeIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ids, err := ListNetworkIDs()
	if err != nil {
		t.Fatalf("ListNetworkIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("ListNetworkIDs() = %v, want empty", ids)
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
