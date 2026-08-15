package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests guard the P0 fix for relative storage paths resolving against
// the process's cwd instead of the config file's directory: `moltnet start`
// run from an unrelated cwd must anchor its default SQLite path under the
// network directory it discovered, not litter the launch directory.

func TestRunServerAnchorsSQLiteStorageUnderHomeNetworkDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")

	networkDir := filepath.Join(home, ".moltnet", "acme")
	if err := os.MkdirAll(networkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(networkDir, "Moltnet"), []byte("version: moltnet.v1\n\nnetwork:\n  id: acme\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	unrelatedCwd := t.TempDir()
	t.Chdir(unrelatedCwd)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := runServer(ctx, nil, "test"); err != nil {
		t.Fatalf("runServer() error = %v", err)
	}

	wantDB := filepath.Join(networkDir, ".moltnet", "moltnet.db")
	if _, err := os.Stat(wantDB); err != nil {
		t.Fatalf("expected sqlite db at %q: %v", wantDB, err)
	}
	if _, err := os.Stat(filepath.Join(unrelatedCwd, ".moltnet")); err == nil {
		t.Fatalf("unexpected .moltnet storage litter under unrelated cwd %q", unrelatedCwd)
	}
}

func TestRunServerAnchorsSQLiteStorageForExplicitConfigFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")

	networkDir := t.TempDir()
	configPath := filepath.Join(networkDir, "Moltnet")
	if err := os.WriteFile(configPath, []byte("version: moltnet.v1\n\nnetwork:\n  id: acme\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	unrelatedCwd := t.TempDir()
	t.Chdir(unrelatedCwd)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := runServer(ctx, []string{"--config", configPath}, "test"); err != nil {
		t.Fatalf("runServer() error = %v", err)
	}

	wantDB := filepath.Join(networkDir, ".moltnet", "moltnet.db")
	if _, err := os.Stat(wantDB); err != nil {
		t.Fatalf("expected sqlite db at %q: %v", wantDB, err)
	}
	if _, err := os.Stat(filepath.Join(unrelatedCwd, ".moltnet")); err == nil {
		t.Fatalf("unexpected .moltnet storage litter under unrelated cwd %q", unrelatedCwd)
	}
}

func TestRunServerKeepsSQLiteStorageNextToCwdMoltnetFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "Moltnet"), []byte("version: moltnet.v1\n\nnetwork:\n  id: acme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := runServer(ctx, nil, "test"); err != nil {
		t.Fatalf("runServer() error = %v", err)
	}

	// Config dir == cwd here, so this must match pre-fix behavior exactly.
	wantDB := filepath.Join(directory, ".moltnet", "moltnet.db")
	if _, err := os.Stat(wantDB); err != nil {
		t.Fatalf("expected sqlite db at %q: %v", wantDB, err)
	}
}
