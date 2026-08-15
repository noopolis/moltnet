package relaydeploy

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr to a pipe for the duration of fn and
// returns everything written to it. relaydeploy has no shared test helper
// package of its own (unlike cmd/moltnet's captureStdout), so this is kept
// local to this file.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	previous := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = previous })

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	os.Stderr = previous
	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(captured)
}

func TestCloudflareTokenPathIsUnderDotMoltnetNextToConfig(t *testing.T) {
	t.Parallel()
	got := CloudflareTokenPath("/home/alice/project/Moltnet")
	want := filepath.Join("/home/alice/project", ".moltnet", "cloudflare.json")
	if got != want {
		t.Fatalf("CloudflareTokenPath() = %q, want %q", got, want)
	}
}

func TestLoadCloudflareTokenMissingFileIsNotAnError(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))

	token, ok, err := LoadCloudflareToken(path)
	if err != nil {
		t.Fatalf("LoadCloudflareToken() error = %v, want nil for a missing file", err)
	}
	if ok {
		t.Fatal("LoadCloudflareToken() ok = true, want false for a missing file")
	}
	if token != "" {
		t.Fatalf("LoadCloudflareToken() token = %q, want empty", token)
	}
}

func TestSaveAndLoadCloudflareTokenRoundTrip(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))

	want := "cf-test-token-value"
	if err := SaveCloudflareToken(path, want); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}

	got, ok, err := LoadCloudflareToken(path)
	if err != nil {
		t.Fatalf("LoadCloudflareToken() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadCloudflareToken() ok = false after a successful save")
	}
	if got != want {
		t.Fatalf("LoadCloudflareToken() = %q, want %q", got, want)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat cloudflare token file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("cloudflare token file mode = %o, want 0600", perm)
		}
	}
}

func TestSaveCloudflareTokenIsAtomicViaRename(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))

	if err := SaveCloudflareToken(path, "first-token"); err != nil {
		t.Fatalf("first SaveCloudflareToken() error = %v", err)
	}
	if err := SaveCloudflareToken(path, "second-token"); err != nil {
		t.Fatalf("second SaveCloudflareToken() error = %v", err)
	}

	got, ok, err := LoadCloudflareToken(path)
	if err != nil || !ok {
		t.Fatalf("LoadCloudflareToken() = (%q, %v, %v)", got, ok, err)
	}
	if got != "second-token" {
		t.Fatalf("LoadCloudflareToken() = %q, want second-token (overwritten)", got)
	}

	// No leftover temp files from the atomic write's rename.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "cloudflare.json" {
			t.Fatalf("unexpected leftover file %q in .moltnet/ after SaveCloudflareToken", entry.Name())
		}
	}
}

func TestSaveCloudflareTokenCreatesParentDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))

	if err := SaveCloudflareToken(path, "token"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".moltnet")); err != nil {
		t.Fatalf("expected .moltnet directory to be created: %v", err)
	}
}

func TestDeleteCloudflareTokenRemovesStoredFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))

	if err := SaveCloudflareToken(path, "token"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}

	removed, err := DeleteCloudflareToken(path)
	if err != nil {
		t.Fatalf("DeleteCloudflareToken() error = %v", err)
	}
	if !removed {
		t.Fatal("DeleteCloudflareToken() removed = false, want true")
	}
	if _, ok, err := LoadCloudflareToken(path); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() after delete = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestDeleteCloudflareTokenMissingFileIsNotAnError(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))

	removed, err := DeleteCloudflareToken(path)
	if err != nil {
		t.Fatalf("DeleteCloudflareToken() error = %v, want nil for a missing file", err)
	}
	if removed {
		t.Fatal("DeleteCloudflareToken() removed = true, want false for a missing file")
	}
}

func TestLoadCloudflareTokenBlankTokenIsTreatedAsNotStored(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"apiToken":"  "}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	token, ok, err := LoadCloudflareToken(path)
	if err != nil {
		t.Fatalf("LoadCloudflareToken() error = %v", err)
	}
	if ok || token != "" {
		t.Fatalf("LoadCloudflareToken() = (%q, %v), want (\"\", false) for a blank stored token", token, ok)
	}
}

func TestLoadCloudflareTokenWarnsWhenGroupOrOtherReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits don't apply on windows")
	}
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))
	if err := SaveCloudflareToken(path, "cf-test-token-value"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	var token string
	stderr := captureStderr(t, func() {
		var ok bool
		var err error
		token, ok, err = LoadCloudflareToken(path)
		if err != nil {
			t.Fatalf("LoadCloudflareToken() error = %v", err)
		}
		if !ok {
			t.Fatal("LoadCloudflareToken() ok = false, want true")
		}
	})
	if token != "cf-test-token-value" {
		t.Fatalf("LoadCloudflareToken() token = %q, want cf-test-token-value", token)
	}
	if !strings.Contains(stderr, path) {
		t.Fatalf("expected a stderr warning naming %q, got %q", path, stderr)
	}
	if strings.Contains(stderr, "cf-test-token-value") {
		t.Fatalf("expected the token value itself never to be printed, got %q", stderr)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("file mode = %o, want unchanged 0644 (LoadCloudflareToken must warn, never chmod)", perm)
	}
}

func TestLoadCloudflareTokenDoesNotWarnForCorrectPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits don't apply on windows")
	}
	directory := t.TempDir()
	path := CloudflareTokenPath(filepath.Join(directory, "Moltnet"))
	if err := SaveCloudflareToken(path, "cf-test-token-value"); err != nil {
		t.Fatalf("SaveCloudflareToken() error = %v", err)
	}

	stderr := captureStderr(t, func() {
		if _, _, err := LoadCloudflareToken(path); err != nil {
			t.Fatalf("LoadCloudflareToken() error = %v", err)
		}
	})
	if stderr != "" {
		t.Fatalf("expected no stderr warning for a 0600 file, got %q", stderr)
	}
}
