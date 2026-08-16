package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunInitCreatesCanonicalFiles(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "net")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"init", directory}, "test"); err != nil {
			t.Fatalf("run() init error = %v", err)
		}
	})

	if !strings.Contains(output, "Initializing") || !strings.Contains(output, "ready") {
		t.Fatalf("unexpected init output %q", output)
	}
	assertFileExists(t, filepath.Join(directory, "Moltnet"))
	assertFileExists(t, filepath.Join(directory, "MoltnetNode"))
}

func TestRunInitReportsExistingFiles(t *testing.T) {
	directory := t.TempDir()
	writeNodeConfig(t, filepath.Join(directory, "Moltnet"), defaultMoltnetConfig("local", "Local Moltnet"))
	writeNodeConfig(t, filepath.Join(directory, "MoltnetNode"), defaultMoltnetNodeConfig("local"))

	// --verbose: the per-file "already exists" breakdown is --verbose-only
	// detail under the quiet-by-default redesign.
	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--verbose", directory}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if !strings.Contains(output, "Moltnet already exists") || !strings.Contains(output, "MoltnetNode already exists") {
		t.Fatalf("unexpected existing init output %q", output)
	}
}

func TestRunInitErrorsOnTooManyArgs(t *testing.T) {
	if err := runInit(context.Background(), []string{"one", "two"}); err == nil {
		t.Fatal("expected invalid init args error")
	}
}

// TestRunInitValidatesArgsBeforeAnimation is item 2's own regression guard:
// `moltnet init a b` used to run the full ~0.9s settle animation
// (printBannerAnimated) before ever reaching the "at most one positional
// path" check, so a doomed command still cost a real user the whole
// animation before showing the error it could have shown instantly. Needs a
// real pty (preparePTYBannerTest, banner_player_test.go) — a plain
// captureStdout pipe never satisfies stdoutIsRealTTY, so printBannerAnimated
// would silently no-op either way and the ordering bug would go unnoticed.
func TestRunInitValidatesArgsBeforeAnimation(t *testing.T) {
	if bannerFramesErr != nil {
		t.Fatalf("bannerFramesErr = %v, want nil", bannerFramesErr)
	}

	buf := preparePTYBannerTest(t, 80)
	home := t.TempDir()
	t.Setenv("HOME", home)

	start := time.Now()
	err := runInit(context.Background(), []string{"one", "two"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error for two positional args")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("runInit() took %v to reject invalid args; want instant, before any animation frame is drawn", elapsed)
	}

	if output := drainedOutput(buf); output != "" {
		t.Fatalf("output = %q, want nothing written at all — the arg-count error must fire before printBannerAnimated", output)
	}
}

func TestRunInitErrorsWhenTargetCannotBeCreated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := runInit(context.Background(), []string{filepath.Join(root, "child")}); err == nil {
		t.Fatal("expected init mkdir error")
	}
}

func TestRunValidateDirectory(t *testing.T) {
	directory := t.TempDir()
	writeNodeConfig(t, filepath.Join(directory, "Moltnet"), defaultMoltnetConfig("local", "Local Moltnet"))
	writeNodeConfig(t, filepath.Join(directory, "MoltnetNode"), defaultMoltnetNodeConfig("local"))

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"validate", directory}, "test"); err != nil {
			t.Fatalf("run() validate error = %v", err)
		}
	})

	if !strings.Contains(output, "validated") {
		t.Fatalf("unexpected validate output %q", output)
	}
}

func TestRunValidateSingleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-node.yaml")
	writeNodeConfig(t, path, defaultMoltnetNodeConfig("local"))

	output := captureStdout(t, func() {
		if err := runValidate([]string{path}); err != nil {
			t.Fatalf("runValidate() error = %v", err)
		}
	})

	if !strings.Contains(output, path) {
		t.Fatalf("unexpected validate output %q", output)
	}
}

func TestRunValidateSingleMoltnetFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Moltnet")
	writeNodeConfig(t, path, defaultMoltnetConfig("local", "Local Moltnet"))

	output := captureStdout(t, func() {
		if err := runValidate([]string{path}); err != nil {
			t.Fatalf("runValidate() Moltnet file error = %v", err)
		}
	})

	if !strings.Contains(output, path) {
		t.Fatalf("unexpected Moltnet validate output %q", output)
	}
}

func TestRunValidateErrorsWhenMissing(t *testing.T) {
	err := runValidate([]string{t.TempDir()})
	if err == nil {
		t.Fatal("expected missing config error")
	}
	if !strings.Contains(err.Error(), "no Moltnet or MoltnetNode config found") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRunValidateErrorsOnTooManyArgs(t *testing.T) {
	if err := runValidate([]string{"one", "two"}); err == nil {
		t.Fatal("expected invalid validate args error")
	}
}

func TestWriteFileIfMissing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Moltnet")
	created, err := writeFileIfMissing(path, "version: moltnet.v1\n")
	if err != nil {
		t.Fatalf("writeFileIfMissing() error = %v", err)
	}
	if !created {
		t.Fatal("expected file to be created")
	}

	created, err = writeFileIfMissing(path, "ignored")
	if err != nil {
		t.Fatalf("writeFileIfMissing() existing error = %v", err)
	}
	if created {
		t.Fatal("expected existing file to be left in place")
	}
}

func TestWriteFileIfMissingErrorsOnInspectFailure(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}

	if _, err := writeFileIfMissing(filepath.Join(parent, "Moltnet"), "version: moltnet.v1\n"); err == nil {
		t.Fatal("expected inspect error")
	}
}

func TestDiscoverValidationTargetsDirectory(t *testing.T) {
	directory := t.TempDir()
	serverPath := filepath.Join(directory, "Moltnet")
	nodePath := filepath.Join(directory, "MoltnetNode")
	writeNodeConfig(t, serverPath, defaultMoltnetConfig("local", "Local Moltnet"))
	writeNodeConfig(t, nodePath, defaultMoltnetNodeConfig("local"))

	discoveredServerPath, discoveredNodePath, err := discoverValidationTargets(directory)
	if err != nil {
		t.Fatalf("discoverValidationTargets() error = %v", err)
	}
	if discoveredServerPath != serverPath || discoveredNodePath != nodePath {
		t.Fatalf("unexpected discovered targets server=%q node=%q", discoveredServerPath, discoveredNodePath)
	}
}

func TestDiscoverValidationTargetsErrorsOnMissingTarget(t *testing.T) {
	t.Parallel()

	if _, _, err := discoverValidationTargets(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing target error")
	}
}

func TestDiscoverConfigsInDirectoryFallbackNames(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	serverPath := filepath.Join(directory, "moltnet.yaml")
	nodePath := filepath.Join(directory, "moltnet-node.yaml")
	writeNodeConfig(t, serverPath, defaultMoltnetConfig("local", "Local Moltnet"))
	writeNodeConfig(t, nodePath, defaultMoltnetNodeConfig("local"))

	discoveredServerPath, discoveredNodePath, err := discoverConfigsInDirectory(directory)
	if err != nil {
		t.Fatalf("discoverConfigsInDirectory() error = %v", err)
	}
	if discoveredServerPath != serverPath || discoveredNodePath != nodePath {
		t.Fatalf("unexpected fallback targets server=%q node=%q", discoveredServerPath, discoveredNodePath)
	}
}

func TestDiscoverValidationTargetsSingleNodeFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "MoltnetNode")
	writeNodeConfig(t, path, defaultMoltnetNodeConfig("local"))

	serverPath, nodePath, err := discoverValidationTargets(path)
	if err != nil {
		t.Fatalf("discoverValidationTargets() error = %v", err)
	}
	if serverPath != "" || nodePath != path {
		t.Fatalf("unexpected targets server=%q node=%q", serverPath, nodePath)
	}
}

func TestDiscoverValidationTargetsErrorsOnInvalidFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatalf("write broken file: %v", err)
	}

	if _, _, err := discoverValidationTargets(path); err == nil {
		t.Fatal("expected invalid file error")
	}
}

func TestFindFirstExistingErrorsOnInspectFailure(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}

	if _, err := findFirstExisting(filepath.Join(parent, "child"), []string{"Moltnet"}); err == nil {
		t.Fatal("expected findFirstExisting inspect error")
	}
}

func TestValidateConfigPathHelpers(t *testing.T) {
	directory := t.TempDir()
	serverPath := filepath.Join(directory, "Moltnet")
	nodePath := filepath.Join(directory, "MoltnetNode")
	writeNodeConfig(t, serverPath, defaultMoltnetConfig("local", "Local Moltnet"))
	writeNodeConfig(t, nodePath, defaultMoltnetNodeConfig("local"))

	if _, err := validateServerConfigPath(serverPath); err != nil {
		t.Fatalf("validateServerConfigPath() error = %v", err)
	}
	if _, err := validateNodeConfigPath(nodePath); err != nil {
		t.Fatalf("validateNodeConfigPath() error = %v", err)
	}
	if _, err := validateServerConfigPath(filepath.Join(directory, "missing")); err == nil {
		t.Fatal("expected validateServerConfigPath() missing error")
	}
	if _, err := validateNodeConfigPath(filepath.Join(directory, "missing")); err == nil {
		t.Fatal("expected validateNodeConfigPath() missing error")
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %q: %v", path, err)
	}
}
