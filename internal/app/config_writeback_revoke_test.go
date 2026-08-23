package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const revokeFixtureYAML = `version: moltnet.v1

network:
  id: acme

auth:
  mode: bearer
  tokens:
    - id: operator
      value: operator-secret
      scopes: [observe, write, admin]
    - id: friend-1234
      value: peer-secret-token
      scopes: [pair]
    - id: friend-5678
      value: other-secret-token
      scopes: [pair]

rooms:
  - id: chat
    federation: [friend-1234]
  - id: lobby
    federation: [friend-1234, friend-5678]
  - id: open-room
    federation: all

pairings:
  - id: friend-1234
    remote_network_id: remote-net
    token: peer-secret-token
    relay:
      url: wss://relay.example/friend-1234
      room: relay-room-1234
      token: relay-secret-1234
  - id: friend-5678
    remote_network_id: other-net
    token: other-secret-token
    relay:
      url: wss://relay.example/friend-5678
      room: relay-room-5678
      token: relay-secret-5678
`

func writeRevokeFixtureConfig(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte(revokeFixtureYAML), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	return path
}

func TestRevokePairingRemovesPairingAndAuthToken(t *testing.T) {
	t.Parallel()

	path := writeRevokeFixtureConfig(t)
	result, err := RevokePairing(path, "friend-1234")
	if err != nil {
		t.Fatalf("RevokePairing() error = %v", err)
	}
	if !result.TokenRemoved {
		t.Fatalf("RevokeResult.TokenRemoved = false, want true: fixture has an auth.tokens[] entry with id %q", "friend-1234")
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	for _, pairing := range config.Pairings {
		if pairing.ID == "friend-1234" {
			t.Fatalf("pairings[] still has revoked pairing %#v", pairing)
		}
	}
	for _, token := range config.Auth.Tokens {
		if token.ID == "friend-1234" {
			t.Fatalf("auth.tokens[] still has revoked peer's credential %#v", token)
		}
	}
	// The other pairing's own token must survive untouched.
	foundOtherToken := false
	for _, token := range config.Auth.Tokens {
		if token.ID == "friend-5678" {
			foundOtherToken = true
		}
	}
	if !foundOtherToken {
		t.Fatalf("unrelated auth.tokens[] entry friend-5678 was removed, want it left alone")
	}
}

func TestRevokePairingStripsListFederationToNoneWhenEmptied(t *testing.T) {
	t.Parallel()

	path := writeRevokeFixtureConfig(t)
	if _, err := RevokePairing(path, "friend-1234"); err != nil {
		t.Fatalf("RevokePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	room := findWrittenRoom(t, config, "chat")
	if room.Federation == nil || room.Federation.Mode != protocol.RoomFederationNone {
		t.Fatalf("chat federation = %#v, want an explicit \"none\" (a re-pairing under the same id must not silently regain access)", room.Federation)
	}
}

func TestRevokePairingLeavesOtherFederationEntriesIntact(t *testing.T) {
	t.Parallel()

	path := writeRevokeFixtureConfig(t)
	if _, err := RevokePairing(path, "friend-1234"); err != nil {
		t.Fatalf("RevokePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	room := findWrittenRoom(t, config, "lobby")
	if room.Federation == nil || !protocol.RoomFederationAllows(room.Federation, "friend-5678") {
		t.Fatalf("lobby federation = %#v, want friend-5678 still allowed", room.Federation)
	}
	if protocol.RoomFederationAllows(room.Federation, "friend-1234") {
		t.Fatalf("lobby federation = %#v, want friend-1234 no longer allowed", room.Federation)
	}
}

func TestRevokePairingLeavesAllFederationAlone(t *testing.T) {
	t.Parallel()

	path := writeRevokeFixtureConfig(t)
	if _, err := RevokePairing(path, "friend-1234"); err != nil {
		t.Fatalf("RevokePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	room := findWrittenRoom(t, config, "open-room")
	if room.Federation == nil || room.Federation.Mode != protocol.RoomFederationAll {
		t.Fatalf("open-room federation = %#v, want untouched \"all\"", room.Federation)
	}
}

func TestRevokePairingUnknownPairingReturnsDistinguishableError(t *testing.T) {
	t.Parallel()

	path := writeRevokeFixtureConfig(t)
	_, err := RevokePairing(path, "does-not-exist")
	if !errors.Is(err, ErrPairingNotFound) {
		t.Fatalf("RevokePairing() error = %v, want ErrPairingNotFound", err)
	}

	// A failed revoke must not touch the file: reload and confirm both
	// pairings are still exactly as they were.
	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Pairings) != 2 {
		t.Fatalf("pairings = %#v, want both untouched after a failed revoke", config.Pairings)
	}
}

// TestRevokePairingReportsTokenNotRemovedOnIDMismatch is the new P2 finding's
// required regression: a pairing whose auth.tokens[] entry uses a different
// id than the pairing itself (a hand-edited config -- every id `pair
// invite`/`pair <code>` ever writes matches by construction) leaves the
// peer's credential live even though the pairing and its federation grants
// are gone. RevokeResult.TokenRemoved must say so instead of the caller
// assuming success.
func TestRevokePairingReportsTokenNotRemovedOnIDMismatch(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	body := `version: moltnet.v1

network:
  id: acme

auth:
  mode: bearer
  tokens:
    - id: operator
      value: operator-secret
      scopes: [observe, write, admin]
    - id: friend-different-token-id
      value: peer-secret-token
      scopes: [pair]

rooms:
  - id: chat
    federation: [friend-1234]

pairings:
  - id: friend-1234
    remote_network_id: remote-net
    token: peer-secret-token
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	result, err := RevokePairing(path, "friend-1234")
	if err != nil {
		t.Fatalf("RevokePairing() error = %v", err)
	}
	if result.TokenRemoved {
		t.Fatalf("RevokeResult.TokenRemoved = true, want false: no auth.tokens[] entry has id %q", "friend-1234")
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	found := false
	for _, token := range config.Auth.Tokens {
		if token.ID == "friend-different-token-id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the mismatched-id token was removed anyway, want it left alone (RevokePairing must not guess): %#v", config.Auth.Tokens)
	}
}

func TestRevokePairingRefusesSymlinkedConfig(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	realPath := filepath.Join(directory, "Moltnet.real")
	if err := os.WriteFile(realPath, []byte(revokeFixtureYAML), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	linkPath := filepath.Join(directory, "Moltnet")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("Symlink(): %v", err)
	}

	if _, err := RevokePairing(linkPath, "friend-1234"); err == nil {
		t.Fatalf("RevokePairing() over a symlinked config error = nil, want refusal")
	}
}
