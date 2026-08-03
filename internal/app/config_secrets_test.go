package app

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidatePairingsRelayModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pairing   protocol.Pairing
		wantError string
	}{
		{
			name:      "both endpoints",
			pairing:   protocol.Pairing{ID: "pair", RemoteBaseURL: "https://remote.example.com", Relay: &protocol.PairingRelay{URL: "wss://relay.example.com"}},
			wantError: "conflicting remote_base_url and relay.url",
		},
		{
			name:      "neither endpoint",
			pairing:   protocol.Pairing{ID: "pair"},
			wantError: "requires exactly one of remote_base_url or relay.url",
		},
		{
			name:      "relay http scheme",
			pairing:   protocol.Pairing{ID: "pair", Relay: &protocol.PairingRelay{URL: "http://relay.example.com"}},
			wantError: `pairings[0].relay.url scheme "http" is unsupported`,
		},
		{
			name:      "relay room required",
			pairing:   protocol.Pairing{ID: "pair", Relay: &protocol.PairingRelay{URL: "wss://relay.example.com", Room: "   "}},
			wantError: "pairings[0] must specify relay.room as a non-empty rendezvous name",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := validatePairings([]protocol.Pairing{test.pairing})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validatePairings() error = %v, want substring %q", err, test.wantError)
			}
		})
	}

	for _, pairing := range []protocol.Pairing{
		{ID: "relay", Relay: &protocol.PairingRelay{URL: "wss://relay.example.com", Room: "lobby"}},
		{ID: "relay_ws", Relay: &protocol.PairingRelay{URL: "ws://relay.example.com", Room: "lobby"}},
		{ID: "remote", RemoteBaseURL: "http://remote.example.com"},
	} {
		if err := validatePairings([]protocol.Pairing{pairing}); err != nil {
			t.Fatalf("validatePairings(%#v) error = %v", pairing, err)
		}
	}
	if err := validateRelayURL("pairings[0].relay.url", "ws:///missing-host"); err == nil {
		t.Fatal("expected relay missing host error")
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
	if !hasPlaintextPairingTokens([]protocol.Pairing{{ID: "pair", Relay: &protocol.PairingRelay{Token: "relay-connect-secret"}}}) {
		t.Fatal("expected plaintext relay token detection")
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
