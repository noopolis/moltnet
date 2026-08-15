package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const writebackFixtureYAML = `
version: moltnet.v1
network:
  id: alice-net
  name: Alice's Moltnet
auth:
  mode: bearer
  tokens:
    - id: existing-token
      value: existing-secret-value
      scopes: [observe]
rooms:
  - id: agora
    federation: none
    credential:
      token: room-secret-value
pairings:
  - id: existing-pair
    remote_network_id: bob-net
    remote_base_url: https://bob.example.com
    token: existing-pair-token
`

func writeWritebackFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(writebackFixtureYAML), 0o600); err != nil {
		t.Fatalf("write fixture config %q: %v", path, err)
	}
}

func newFriendPairing() (PairingWriteback, AuthTokenWriteback) {
	pairing := PairingWriteback{
		ID:              "friend-net",
		RemoteNetworkID: "bob-net2",
		Token:           "new-pairing-token",
		Relay: &PairingRelayWriteback{
			URL:   "wss://relay.example.dev",
			Room:  "room-xyz",
			Token: "relay-secret-token",
		},
	}
	authToken := AuthTokenWriteback{
		ID:      "friend-net",
		Value:   "new-pairing-token",
		Network: "bob-net2",
		Scopes:  []string{"pair"},
	}
	return pairing, authToken
}

func TestWritePairingReloadsWithPlaintextSecrets(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, nil, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if strings.Contains(string(raw), "[REDACTED]") {
		t.Fatalf("written config contains redacted secrets:\n%s", raw)
	}

	config, err := LoadConfigForPath(path, "1.2.3")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	if len(config.Auth.Tokens) != 2 {
		t.Fatalf("expected 2 auth tokens, got %#v", config.Auth.Tokens)
	}
	foundExisting, foundNew := false, false
	for _, token := range config.Auth.Tokens {
		switch token.ID {
		case "existing-token":
			if token.Value != "existing-secret-value" {
				t.Fatalf("existing auth token value = %q, want plaintext", token.Value)
			}
			foundExisting = true
		case "friend-net":
			if token.Value != "new-pairing-token" || token.Network != "bob-net2" {
				t.Fatalf("unexpected new auth token %#v", token)
			}
			foundNew = true
		}
	}
	if !foundExisting || !foundNew {
		t.Fatalf("missing expected auth tokens: %#v", config.Auth.Tokens)
	}

	if len(config.Rooms) != 1 || config.Rooms[0].Credential == nil ||
		config.Rooms[0].Credential.Token.Reveal() != "room-secret-value" {
		t.Fatalf("room credential secret was not preserved: %#v", config.Rooms)
	}

	if len(config.Pairings) != 2 {
		t.Fatalf("expected 2 pairings, got %#v", config.Pairings)
	}
	foundExistingPairing, foundNewPairing := false, false
	for _, p := range config.Pairings {
		switch p.ID {
		case "existing-pair":
			if p.Token.Reveal() != "existing-pair-token" {
				t.Fatalf("existing pairing token = %q, want plaintext", p.Token.Reveal())
			}
			foundExistingPairing = true
		case "friend-net":
			if p.Token.Reveal() != "new-pairing-token" || p.RemoteNetworkID != "bob-net2" {
				t.Fatalf("unexpected new pairing %#v", p)
			}
			if p.Relay == nil || p.Relay.Room != "room-xyz" || p.Relay.Token.Reveal() != "relay-secret-token" {
				t.Fatalf("unexpected new pairing relay %#v", p.Relay)
			}
			foundNewPairing = true
		}
	}
	if !foundExistingPairing || !foundNewPairing {
		t.Fatalf("missing expected pairings: %#v", config.Pairings)
	}
}

func TestWritePairingSupportsJSONFormat(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "moltnet.json")
	if err := os.WriteFile(path, []byte(`{"version":"moltnet.v1","network":{"id":"alice-net","name":"Alice"}}`), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, nil, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Pairings) != 1 || config.Pairings[0].Token.Reveal() != "new-pairing-token" {
		t.Fatalf("unexpected pairings after JSON writeback: %#v", config.Pairings)
	}
}

func TestWritePairingRefusesDuplicateWithoutForce(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	pairing := PairingWriteback{ID: "existing-pair", RemoteNetworkID: "bob-net", Token: "another-token"}
	authToken := AuthTokenWriteback{ID: "existing-pair", Value: "another-token", Scopes: []string{"pair"}}

	err := WritePairing(path, pairing, authToken, nil, false)
	if !errors.Is(err, ErrPairingExists) {
		t.Fatalf("WritePairing() error = %v, want ErrPairingExists", err)
	}

	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read config after refused write: %v", readErr)
	}
	if !strings.Contains(string(raw), "existing-pair-token") {
		t.Fatalf("original pairing token was lost after refused write:\n%s", raw)
	}
}

func TestWritePairingOverwritesWithForce(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	pairing := PairingWriteback{
		ID:              "existing-pair",
		RemoteNetworkID: "bob-net-new",
		Token:           "rotated-token",
		Relay:           &PairingRelayWriteback{URL: "wss://relay.example.dev", Room: "room-xyz", Token: "relay-secret-token"},
	}
	authToken := AuthTokenWriteback{ID: "existing-pair", Value: "rotated-token", Scopes: []string{"pair"}}

	if err := WritePairing(path, pairing, authToken, nil, true); err != nil {
		t.Fatalf("WritePairing() with force error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Pairings) != 1 {
		t.Fatalf("expected force overwrite to keep a single pairing, got %#v", config.Pairings)
	}
	if config.Pairings[0].RemoteNetworkID != "bob-net-new" || config.Pairings[0].Token.Reveal() != "rotated-token" {
		t.Fatalf("pairing was not overwritten: %#v", config.Pairings[0])
	}
}

func TestWritePairingIsAtomicPrivateMode(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod fixture config: %v", err)
	}

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, nil, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("written config mode = %v, want 0600", info.Mode().Perm())
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".moltnet-config-") {
			t.Fatalf("leftover temp file %q was not cleaned up", entry.Name())
		}
	}
}

func TestWritePairingRejectsSymlinkedConfig(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	writeWritebackFixture(t, target)

	link := filepath.Join(directory, "Moltnet")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	pairing, authToken := newFriendPairing()
	if err := WritePairing(link, pairing, authToken, nil, false); err == nil {
		t.Fatal("expected symlink rejection error")
	}
}

func newOperatorToken() AuthTokenWriteback {
	return AuthTokenWriteback{
		ID:     "operator",
		Value:  "operator-token-value",
		Scopes: []string{"observe", "write", "admin", "pair"},
	}
}

func TestAddOperatorTokenCreatesAuthWhenAbsent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte("version: moltnet.v1\nnetwork:\n  id: alice-net\n  name: Alice's Moltnet\n"), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	if err := AddOperatorToken(path, newOperatorToken()); err != nil {
		t.Fatalf("AddOperatorToken() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if config.Auth.Mode != "bearer" {
		t.Fatalf("auth.mode = %q, want bearer", config.Auth.Mode)
	}
	if len(config.Auth.Tokens) != 1 || config.Auth.Tokens[0].ID != "operator" || config.Auth.Tokens[0].Value != "operator-token-value" {
		t.Fatalf("unexpected auth tokens: %#v", config.Auth.Tokens)
	}
}

func TestAddOperatorTokenAppendsWhenTokensEmpty(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	contents := "version: moltnet.v1\nnetwork:\n  id: alice-net\n  name: Alice's Moltnet\nauth:\n  mode: bearer\n  tokens: []\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	if err := AddOperatorToken(path, newOperatorToken()); err != nil {
		t.Fatalf("AddOperatorToken() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Auth.Tokens) != 1 || config.Auth.Tokens[0].ID != "operator" {
		t.Fatalf("unexpected auth tokens: %#v", config.Auth.Tokens)
	}
}

func TestAddOperatorTokenRefusesWhenTokensExist(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}

	err = AddOperatorToken(path, newOperatorToken())
	if !errors.Is(err, ErrAuthTokensExist) {
		t.Fatalf("AddOperatorToken() error = %v, want ErrAuthTokensExist", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after refused write: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("config was modified despite ErrAuthTokensExist:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestAddOperatorTokenSupportsJSONFormat(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "moltnet.json")
	if err := os.WriteFile(path, []byte(`{"version":"moltnet.v1","network":{"id":"alice-net","name":"Alice"}}`), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	if err := AddOperatorToken(path, newOperatorToken()); err != nil {
		t.Fatalf("AddOperatorToken() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if config.Auth.Mode != "bearer" || len(config.Auth.Tokens) != 1 || config.Auth.Tokens[0].ID != "operator" {
		t.Fatalf("unexpected config after JSON writeback: %#v", config.Auth)
	}
}

func TestAddOperatorTokenRejectsSymlinkedConfig(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	writeWritebackFixture(t, target)

	link := filepath.Join(directory, "Moltnet")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if err := AddOperatorToken(link, newOperatorToken()); err == nil {
		t.Fatal("expected symlink rejection error")
	}
}
