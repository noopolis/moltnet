package relaydeploy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ensureEmbeddedRelayBundleFresh mirrors internal/transport's
// ensureEmbeddedWebBundleFresh for relay/dist: it fails when any relay
// Worker source is newer than the committed bundle, so a stale
// `npm run bundle` in relay/ is caught in CI without needing Node.
func ensureEmbeddedRelayBundleFresh(sourceRoot, outputRoot string) error {
	var sources []string
	if err := filepath.WalkDir(filepath.Join(sourceRoot, "src"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		sources = append(sources, path)
		return nil
	}); err != nil {
		return fmt.Errorf("scan relay sources: %w", err)
	}
	for _, name := range []string{"package.json", "wrangler.jsonc", "build.mjs"} {
		sources = append(sources, filepath.Join(sourceRoot, name))
	}
	if len(sources) == 0 {
		return fmt.Errorf("relay bundle freshness scan found zero source files")
	}

	var outputs []string
	if err := filepath.WalkDir(outputRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			outputs = append(outputs, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("scan relay bundle: %w", err)
	}
	if len(outputs) == 0 {
		return fmt.Errorf("relay bundle freshness scan found zero output files")
	}

	oldestOutput := outputs[0]
	oldestOutputInfo, err := os.Stat(oldestOutput)
	if err != nil {
		return fmt.Errorf("stat relay bundle output %s: %w", oldestOutput, err)
	}
	for _, output := range outputs[1:] {
		info, statErr := os.Stat(output)
		if statErr != nil {
			return fmt.Errorf("stat relay bundle output %s: %w", output, statErr)
		}
		if info.ModTime().Before(oldestOutputInfo.ModTime()) {
			oldestOutput, oldestOutputInfo = output, info
		}
	}

	for _, source := range sources {
		info, statErr := os.Stat(source)
		if statErr != nil {
			return fmt.Errorf("stat relay source %s: %w", source, statErr)
		}
		// Git checkouts can stamp adjacent source and generated files a few
		// milliseconds apart even when the committed bundle is current. Keep
		// a small filesystem-timestamp tolerance, same as the web bundle
		// freshness check.
		if info.ModTime().After(oldestOutputInfo.ModTime().Add(2 * time.Second)) {
			return fmt.Errorf(
				"relay bundle stale: source %s mtime %s is newer than oldest output %s mtime %s; run npm run bundle in relay/",
				source, info.ModTime().Format(time.RFC3339Nano), oldestOutput, oldestOutputInfo.ModTime().Format(time.RFC3339Nano),
			)
		}
	}
	return nil
}

// embeddedRelayRoot returns relay/ and relay/dist relative to this test
// file, so the check runs against the real committed bundle regardless of
// the working directory `go test` is invoked from.
func embeddedRelayRoot() (string, string) {
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(filename), "..", "..", "relay")
	return root, filepath.Join(root, "dist")
}

// TestEmbeddedRelayBundleIsFresh guards the actual committed relay/dist
// against drifting from relay/src, wrangler.jsonc, and build.mjs.
func TestEmbeddedRelayBundleIsFresh(t *testing.T) {
	t.Parallel()

	relayRoot, relayDist := embeddedRelayRoot()
	if err := ensureEmbeddedRelayBundleFresh(relayRoot, relayDist); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedRelayBundleFreshnessRejectsEmptyOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "server.ts"), []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "wrangler.jsonc", "build.mjs"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err := ensureEmbeddedRelayBundleFresh(root, filepath.Join(root, "dist"))
	if err == nil || !strings.Contains(err.Error(), "zero output files") {
		t.Fatalf("expected empty-output vacuity failure, got %v", err)
	}
}

func TestEmbeddedRelayBundleFreshnessAllowsCheckoutTimestampSkew(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "src", "server.ts")
	output := filepath.Join(root, "dist", "worker.js")
	for _, directory := range []string{filepath.Dir(source), filepath.Dir(output)} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{source, output} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"package.json", "wrangler.jsonc", "build.mjs"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	checkoutTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(output, checkoutTime, checkoutTime); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"src/server.ts", "package.json", "wrangler.jsonc", "build.mjs"} {
		path := filepath.Join(root, name)
		if err := os.Chtimes(path, checkoutTime.Add(time.Second), checkoutTime.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureEmbeddedRelayBundleFresh(root, filepath.Join(root, "dist")); err != nil {
		t.Fatalf("expected checkout timestamp skew to pass: %v", err)
	}

	if err := os.Chtimes(source, checkoutTime.Add(3*time.Second), checkoutTime.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := ensureEmbeddedRelayBundleFresh(root, filepath.Join(root, "dist")); err == nil || !strings.Contains(err.Error(), "relay bundle stale") {
		t.Fatalf("expected genuinely stale source to fail, got %v", err)
	}
}
