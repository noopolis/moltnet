package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunServerResolvesNetworkByID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")

	acmeDir := filepath.Join(home, ".moltnet", "acme")
	betaDir := filepath.Join(home, ".moltnet", "beta")
	if err := os.MkdirAll(acmeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(betaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acmeDir, "Moltnet"), []byte("version: moltnet.v1\n\nnetwork:\n  id: acme\n  name: Acme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(betaDir, "Moltnet"), []byte("version: moltnet.v1\n\nnetwork:\n  id: beta\n  name: Beta\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// With two networks present and no --id, start must refuse to guess.
	if err := runServer(context.Background(), nil, "test"); err == nil {
		t.Fatal("expected an error when several networks exist and --id is not given")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := runServer(ctx, []string{"--id", "beta"}, "test"); err != nil {
		t.Fatalf("runServer() --id beta error = %v", err)
	}
}

func TestRunNodeResolvesNetworkByID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	acmeDir := filepath.Join(home, ".moltnet", "acme")
	if err := os.MkdirAll(acmeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acmeDir, "MoltnetNode"), []byte(defaultMoltnetNodeConfig("acme", "http://127.0.0.1:8787")), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := runNode(ctx, []string{"--id", "acme"}); err != nil {
		t.Fatalf("runNode() --id acme error = %v", err)
	}
}
