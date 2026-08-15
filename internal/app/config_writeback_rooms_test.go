package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// This file covers WritePairing's roomIDs handling: the rooms[] upsert and
// federation wiring PLAN.md phase 3 adds. It shares fixtures and helpers
// (writeWritebackFixture, newFriendPairing) with config_writeback_test.go,
// which covers the pairing/auth-token half of the same write.

func findWrittenRoom(t *testing.T, config Config, id string) RoomConfig {
	t.Helper()
	for _, room := range config.Rooms {
		if room.ID == id {
			return room
		}
	}
	t.Fatalf("room %q not found in %#v", id, config.Rooms)
	return RoomConfig{}
}

func TestWritePairingCreatesSharedRoomWhenAbsent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, []string{"chat"}, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	if len(config.Rooms) != 2 {
		t.Fatalf("expected the pre-existing room plus one created room, got %#v", config.Rooms)
	}
	created := findWrittenRoom(t, config, "chat")
	if !protocol.RoomFederationAllows(created.Federation, pairing.ID) {
		t.Fatalf("created room federation does not allow pairing %q: %#v", pairing.ID, created.Federation)
	}

	// The pre-existing room must be untouched.
	existing := findWrittenRoom(t, config, "agora")
	if existing.Federation == nil || existing.Federation.Mode != protocol.RoomFederationNone {
		t.Fatalf("pre-existing room federation was modified: %#v", existing.Federation)
	}
}

func TestWritePairingUpgradesNoneFederationToList(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, []string{"agora"}, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	room := findWrittenRoom(t, config, "agora")
	if room.Federation == nil || room.Federation.Mode != protocol.RoomFederationList {
		t.Fatalf("expected agora federation to become a list, got %#v", room.Federation)
	}
	if !protocol.RoomFederationAllows(room.Federation, pairing.ID) {
		t.Fatalf("agora federation does not allow pairing %q: %#v", pairing.ID, room.Federation)
	}
	if room.Credential == nil || room.Credential.Token.Reveal() != "room-secret-value" {
		t.Fatalf("agora's room credential secret was not preserved: %#v", room.Credential)
	}
}

const roomFederationListFixtureYAML = `
version: moltnet.v1
network:
  id: alice-net
  name: Alice's Moltnet
auth:
  mode: bearer
rooms:
  - id: shared
    federation: [other-pair]
pairings:
  - id: other-pair
    remote_network_id: other-net
    remote_base_url: https://other.example.com
    token: other-pair-token
`

func TestWritePairingAppendsToExistingFederationList(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte(roomFederationListFixtureYAML), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, []string{"shared"}, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	room := findWrittenRoom(t, config, "shared")
	if room.Federation == nil || len(room.Federation.Pairings) != 2 {
		t.Fatalf("expected shared federation to have 2 pairings, got %#v", room.Federation)
	}
	if !protocol.RoomFederationAllows(room.Federation, "other-pair") {
		t.Fatalf("shared federation lost the pre-existing pairing: %#v", room.Federation)
	}
	if !protocol.RoomFederationAllows(room.Federation, pairing.ID) {
		t.Fatalf("shared federation does not allow the new pairing %q: %#v", pairing.ID, room.Federation)
	}
}

func TestWritePairingSharedRoomFederationEntryIsIdempotent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte(roomFederationListFixtureYAML), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, []string{"shared"}, false); err != nil {
		t.Fatalf("first WritePairing() error = %v", err)
	}
	// Re-running with --force must fully replace the pairing without
	// duplicating its entry in the room's federation list.
	if err := WritePairing(path, pairing, authToken, []string{"shared"}, true); err != nil {
		t.Fatalf("second WritePairing() with force error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	room := findWrittenRoom(t, config, "shared")
	if room.Federation == nil || len(room.Federation.Pairings) != 2 {
		t.Fatalf("expected exactly 2 pairings after a re-run, got %#v", room.Federation)
	}
}

const roomFederationAllFixtureYAML = `
version: moltnet.v1
network:
  id: alice-net
  name: Alice's Moltnet
auth:
  mode: bearer
rooms:
  - id: open-room
    federation: all
`

func TestWritePairingLeavesAllFederationAlone(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte(roomFederationAllFixtureYAML), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, []string{"open-room"}, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	room := findWrittenRoom(t, config, "open-room")
	if room.Federation == nil || room.Federation.Mode != protocol.RoomFederationAll {
		t.Fatalf("expected open-room federation to stay \"all\", got %#v", room.Federation)
	}
	if len(room.Federation.Pairings) != 0 {
		t.Fatalf("expected \"all\" federation to name no pairings, got %#v", room.Federation.Pairings)
	}
}

func TestWritePairingRejectsInvalidRoomID(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}

	pairing, authToken := newFriendPairing()
	err = WritePairing(path, pairing, authToken, []string{"bad room id!"}, false)
	if err == nil {
		t.Fatal("expected error for invalid room id")
	}
	if !strings.Contains(err.Error(), "bad room id!") {
		t.Fatalf("expected error to name the bad id, got %q", err.Error())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after refused write: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("WritePairing wrote to config despite invalid room id:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestWritePairingIgnoresBlankAndDuplicateRoomIDs(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, []string{" chat ", "chat", "", "  "}, false); err != nil {
		t.Fatalf("WritePairing() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Rooms) != 2 {
		t.Fatalf("expected exactly one created room despite duplicate/blank input, got %#v", config.Rooms)
	}
}
