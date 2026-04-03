package bridgeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePrivateConfigMode(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	securePath := filepath.Join(directory, "secure.json")
	if err := os.WriteFile(securePath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateConfigMode(securePath); err != nil {
		t.Fatalf("validatePrivateConfigMode() secure error = %v", err)
	}

	insecurePath := filepath.Join(directory, "insecure.json")
	if err := os.WriteFile(insecurePath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validatePrivateConfigMode(insecurePath)
	if err == nil || !strings.Contains(err.Error(), "must not be group/world readable") {
		t.Fatalf("expected insecure permissions error, got %v", err)
	}

	err = validatePrivateConfigMode(filepath.Join(directory, "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "stat bridge config") {
		t.Fatalf("expected stat error, got %v", err)
	}

	targetPath := filepath.Join(directory, "target.json")
	if err := os.WriteFile(targetPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(directory, "link.json")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatal(err)
	}
	err = validatePrivateConfigMode(linkPath)
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}
