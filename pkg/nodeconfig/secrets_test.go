package nodeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePrivateConfigMode(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "MoltnetNode")
	if err := os.WriteFile(path, []byte("version: moltnet.node.v1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := validatePrivateConfigMode(path); err != nil {
		t.Fatalf("validatePrivateConfigMode() private error = %v", err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod public config: %v", err)
	}
	if err := validatePrivateConfigMode(path); err == nil {
		t.Fatal("expected public config mode error")
	}
}

func TestValidatePrivateConfigModeRejectsSymlinks(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "MoltnetNode")
	if err := os.WriteFile(target, []byte("version: moltnet.node.v1\n"), 0o600); err != nil {
		t.Fatalf("write target config: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := validatePrivateConfigMode(link); err == nil {
		t.Fatal("expected symlink config to be rejected")
	}
}
