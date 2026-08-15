package nodeconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverPath(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	writeNodeConfig(t, filepath.Join(directory, DefaultPath), "moltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: local\n")

	path, ok, err := DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath() error = %v", err)
	}
	if !ok || path != DefaultPath {
		t.Fatalf("unexpected discovery result path=%q ok=%v", path, ok)
	}
}

func TestDiscoverPathExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	writeNodeConfig(t, path, "moltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: local\n")

	discovered, ok, err := DiscoverPath(path)
	if err != nil {
		t.Fatalf("DiscoverPath() explicit error = %v", err)
	}
	if !ok || discovered != path {
		t.Fatalf("unexpected explicit discovery path=%q ok=%v", discovered, ok)
	}
}

func TestDiscoverPathSupportsFallbackNamesAndMissingConfig(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	if path, ok, err := DiscoverPath(""); err != nil || ok || path != "" {
		t.Fatalf("expected no config, got path=%q ok=%v err=%v", path, ok, err)
	}

	writeNodeConfig(t, filepath.Join(directory, defaultYAMLAlt), "moltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: local\n")
	path, ok, err := DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath() fallback error = %v", err)
	}
	if !ok || path != defaultYAMLAlt {
		t.Fatalf("unexpected fallback discovery path=%q ok=%v", path, ok)
	}
}

func TestDiscoverPathDirectoryError(t *testing.T) {
	directory := t.TempDir()
	if _, _, err := DiscoverPath(directory); err == nil {
		t.Fatal("expected directory error")
	}
}

// TestDiscoverPathSkipsBinaryShadowedCandidate covers the binary-content
// guard: a discovered (not explicitly named) MoltnetNode candidate that
// fails the text sniff — a stray compiled binary shadowing the filename on
// a case-insensitive filesystem — is skipped with a warning and discovery
// falls through to the next candidate, exactly as if it were never there.
func TestDiscoverPathSkipsBinaryShadowedCandidate(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	if err := os.WriteFile(filepath.Join(directory, DefaultPath), binaryLikeCandidateContents(), 0o755); err != nil {
		t.Fatalf("write binary candidate: %v", err)
	}
	writeNodeConfig(t, filepath.Join(directory, defaultYAMLAlt), "moltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: local\n")

	var warnings bytes.Buffer
	original := configWarnWriter
	configWarnWriter = &warnings
	defer func() { configWarnWriter = original }()

	path, ok, err := DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath() error = %v", err)
	}
	if !ok || path != defaultYAMLAlt {
		t.Fatalf("DiscoverPath() = (%q, %v), want (%q, true)", path, ok, defaultYAMLAlt)
	}
	if !strings.Contains(warnings.String(), DefaultPath) || !strings.Contains(warnings.String(), "not a text config file") {
		t.Fatalf("expected a binary-shadow warning naming %q, got %q", DefaultPath, warnings.String())
	}
}

// TestLoadFileExplicitBinaryPathGetsClearError covers an explicitly named
// MoltnetNode path (never skipped by discovery): pointing it at binary
// bytes must still fail, but with a message naming the binary-file
// suspicion instead of a raw YAML/JSON decode error.
func TestLoadFileExplicitBinaryPathGetsClearError(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	if err := os.WriteFile(path, binaryLikeCandidateContents(), 0o755); err != nil {
		t.Fatalf("write binary config: %v", err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected an error loading a binary MoltnetNode config")
	}
	if !strings.Contains(err.Error(), "does not look like a text config file") {
		t.Fatalf("expected a binary-file-suspicion error, got %v", err)
	}
}

// binaryLikeCandidateContents returns bytes that fail the text sniff (a NUL
// byte within the first 512), standing in for a stray compiled binary
// shadowing a config filename on a case-insensitive filesystem.
func binaryLikeCandidateContents() []byte {
	contents := []byte("\x7fELF\x02\x01\x01\x00")
	return append(contents, make([]byte, 32)...)
}
