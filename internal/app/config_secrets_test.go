package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestValidatePairingsAndRemoteURL(t *testing.T) {
	t.Parallel()

	valid := []protocol.Pairing{{
		ID:              "pair_remote",
		RemoteBaseURL:   "https://remote.example.com",
		RemoteNetworkID: "remote",
	}}
	if err := validatePairings(valid); err != nil {
		t.Fatalf("validatePairings() error = %v", err)
	}
	if err := validateRemoteURL("pairings[0].remote_base_url", "ftp://remote.example.com"); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
	if err := validateRemoteURL("pairings[0].remote_base_url", "https:///missing-host"); err == nil {
		t.Fatal("expected missing host error")
	}
	if err := validatePairings([]protocol.Pairing{{RemoteBaseURL: "https://remote.example.com"}}); err == nil {
		t.Fatal("expected missing id error")
	}
}

func TestPlaintextTokenDetection(t *testing.T) {
	t.Parallel()

	if hasPlaintextPairingTokens(nil) {
		t.Fatal("expected empty pairings to report no tokens")
	}
	if !hasPlaintextPairingTokens([]protocol.Pairing{{ID: "pair", Token: "secret"}}) {
		t.Fatal("expected plaintext pairing token detection")
	}
	if hasPlaintextAuthTokens(rawAuthConfig{}) {
		t.Fatal("expected empty auth config to report no tokens")
	}
	if !hasPlaintextAuthTokens(rawAuthConfig{
		Tokens: []rawAuthTokenConfig{{ID: "operator", Value: "secret"}},
	}) {
		t.Fatal("expected plaintext auth token detection")
	}
}

func TestValidatePrivateConfigMode(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte("version: moltnet.v1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod private config: %v", err)
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
	link := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(target, []byte("version: moltnet.v1\n"), 0o600); err != nil {
		t.Fatalf("write target config: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := validatePrivateConfigMode(link); err == nil {
		t.Fatal("expected symlink config to be rejected")
	}
}
