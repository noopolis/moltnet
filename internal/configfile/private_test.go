package configfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePrivateMode(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	securePath := filepath.Join(directory, "secure.json")
	if err := os.WriteFile(securePath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateMode(securePath, "test config", "secrets", "tokens"); err != nil {
		t.Fatalf("ValidatePrivateMode() secure error = %v", err)
	}

	insecurePath := filepath.Join(directory, "insecure.json")
	if err := os.WriteFile(insecurePath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidatePrivateMode(insecurePath, "test config", "secrets", "tokens")
	if err == nil || !strings.Contains(err.Error(), "test config") || !strings.Contains(err.Error(), "must not be group/world readable when tokens are present") {
		t.Fatalf("expected insecure permissions error, got %v", err)
	}

	err = ValidatePrivateMode(filepath.Join(directory, "missing.json"), "test config", "secrets", "tokens")
	if err == nil || !strings.Contains(err.Error(), "stat test config") {
		t.Fatalf("expected stat error, got %v", err)
	}

	targetPath := filepath.Join(directory, "target.json")
	if err := os.WriteFile(targetPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(directory, "link.json")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	err = ValidatePrivateMode(linkPath, "test config", "secrets", "tokens")
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink when secrets are present") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestFormatForPath(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"config.json": "json",
		"config.JSON": "json",
		"config.yaml": "yaml",
		"config.yml":  "yaml",
		"config":      "yaml",
	}
	for path, want := range tests {
		if got := FormatForPath(path); got != want {
			t.Fatalf("FormatForPath(%q) = %q, want %q", path, got, want)
		}
	}
}
