package transport

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

func ensureEmbeddedWebBundleFresh(sourceRoot, outputRoot string) error {
	var sources []string
	addSource := func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		sources = append(sources, path)
		return nil
	}
	if err := filepath.WalkDir(filepath.Join(sourceRoot, "src"), addSource); err != nil {
		return fmt.Errorf("scan web sources: %w", err)
	}
	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		return fmt.Errorf("scan web root: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "index.html" || entry.Name() == "package.json" || strings.HasPrefix(entry.Name(), "vite.config.") {
			if !entry.IsDir() && (entry.Name() == "index.html" || entry.Name() == "package.json" || strings.HasPrefix(entry.Name(), "vite.config.")) {
				sources = append(sources, filepath.Join(sourceRoot, entry.Name()))
			}
		}
	}
	if len(sources) == 0 {
		return fmt.Errorf("web bundle freshness scan found zero source files")
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
		return fmt.Errorf("scan web bundle: %w", err)
	}
	if len(outputs) == 0 {
		return fmt.Errorf("web bundle freshness scan found zero output files")
	}

	oldestOutput := outputs[0]
	oldestOutputInfo, err := os.Stat(oldestOutput)
	if err != nil {
		return fmt.Errorf("stat web bundle output %s: %w", oldestOutput, err)
	}
	for _, output := range outputs[1:] {
		info, statErr := os.Stat(output)
		if statErr != nil {
			return fmt.Errorf("stat web bundle output %s: %w", output, statErr)
		}
		if info.ModTime().Before(oldestOutputInfo.ModTime()) {
			oldestOutput, oldestOutputInfo = output, info
		}
	}

	for _, source := range sources {
		info, statErr := os.Stat(source)
		if statErr != nil {
			return fmt.Errorf("stat web source %s: %w", source, statErr)
		}
		if info.ModTime().After(oldestOutputInfo.ModTime()) {
			return fmt.Errorf("web bundle stale: source %s mtime %s is newer than oldest output %s mtime %s; run npm run build in ecosystem/moltnet/web", source, info.ModTime().Format(time.RFC3339Nano), oldestOutput, oldestOutputInfo.ModTime().Format(time.RFC3339Nano))
		}
	}
	return nil
}

func embeddedWebRoot() (string, string) {
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(filename), "..", "..", "web")
	return root, filepath.Join(root, "dist")
}

func TestEmbeddedWebBundleFreshnessRejectsEmptyOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.tsx"), []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := ensureEmbeddedWebBundleFresh(root, filepath.Join(root, "dist"))
	if err == nil || !strings.Contains(err.Error(), "zero output files") {
		t.Fatalf("expected empty-output vacuity failure, got %v", err)
	}
}
