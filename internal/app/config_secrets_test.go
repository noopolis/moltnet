package app

import (
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
