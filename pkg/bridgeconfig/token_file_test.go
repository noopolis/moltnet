package bridgeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTokenFileCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".moltnet", "alpha.token")
	if err := WriteTokenFile(path, "magt_v1_secret"); err != nil {
		t.Fatalf("WriteTokenFile() error = %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(contents) != "magt_v1_secret\n" {
		t.Fatalf("unexpected token contents %q", contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestPrepareTokenFileWriteChecksDirectoryBeforeClaim(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	err := PrepareTokenFileWrite(filepath.Join(blocker, "agent.token"))
	if err == nil {
		t.Fatal("expected preflight error")
	}

	path := filepath.Join(dir, ".moltnet", "agent.token")
	if err := PrepareTokenFileWrite(path); err != nil {
		t.Fatalf("PrepareTokenFileWrite() error = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("token directory was not prepared: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("preflight should not create the token file, err=%v", err)
	}
}

func TestReadTokenFileRejectsUnsafeFiles(t *testing.T) {
	dir := t.TempDir()
	insecure := filepath.Join(dir, "insecure.token")
	if err := os.WriteFile(insecure, []byte("secret\n"), 0o644); err != nil {
		t.Fatalf("write insecure token: %v", err)
	}
	if _, err := ReadTokenFile(insecure); err == nil || !strings.Contains(err.Error(), "group/world readable") {
		t.Fatalf("expected insecure file error, got %v", err)
	}
	if err := WriteTokenFile(insecure, "new-secret"); err == nil || !strings.Contains(err.Error(), "group/world readable") {
		t.Fatalf("expected insecure write error, got %v", err)
	}

	target := filepath.Join(dir, "target.token")
	if err := os.WriteFile(target, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write target token: %v", err)
	}
	link := filepath.Join(dir, "link.token")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ReadTokenFile(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink read error, got %v", err)
	}
	if err := WriteTokenFile(link, "new-secret"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink write error, got %v", err)
	}
}
