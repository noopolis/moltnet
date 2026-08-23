package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const roomWritebackFixtureYAML = `
version: moltnet.v1
network:
  id: alice-net
  name: Alice's Moltnet
auth:
  mode: bearer
  tokens:
    - id: operator
      value: operator-secret
      scopes: [observe, write, admin, pair]
rooms: []
pairings: []
`

func writeRoomWritebackFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(roomWritebackFixtureYAML), 0o600); err != nil {
		t.Fatalf("write fixture config %q: %v", path, err)
	}
}

// TestWriteRoomDefaultsToExplicitNoneFederation is PLAN.md 7A's federation
// rule gate: a room written without --federation must land as an explicit
// "none" stance, not a nil one, so validateRoomFederationStances never trips
// the first time a pairing is added to the same file (the exact landmine
// PLAN.md 7A.federation describes).
func TestWriteRoomDefaultsToExplicitNoneFederation(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeRoomWritebackFixture(t, path)

	room := RoomWriteback{ID: "chat", Federation: protocol.DefaultRoomFederation()}
	if err := WriteRoom(path, room); err != nil {
		t.Fatalf("WriteRoom() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after WriteRoom = %v", err)
	}
	if len(config.Rooms) != 1 {
		t.Fatalf("expected exactly one room, got %#v", config.Rooms)
	}
	room0 := config.Rooms[0]
	if room0.Federation == nil || room0.Federation.Mode != protocol.RoomFederationNone {
		t.Fatalf("room federation = %#v, want an explicit {Mode: none}", room0.Federation)
	}

	// The golden test: a config containing this room must still reload
	// cleanly once a pairing is added — the failure
	// validateRoomFederationStances exists to catch is a *nil* stance, which
	// this room must never have.
	pairing, authToken := newFriendPairing()
	if err := WritePairing(path, pairing, authToken, nil, false); err != nil {
		t.Fatalf("WritePairing() after WriteRoom = %v", err)
	}
	if _, err := LoadConfigForPath(path, ""); err != nil {
		t.Fatalf("LoadConfigForPath() after adding a pairing = %v (this is exactly the landmine PLAN.md 7A.federation describes)", err)
	}
}

// TestWriteRoomPersistsFullShape asserts WriteRoom writes every field
// `room create` needs — name, visibility, write_policy, members, and a
// plaintext credential — not just {id, federation}, which is why
// upsertSharedRoom (config_writeback.go) is the wrong helper to reuse
// (PLAN.md 7A.1).
func TestWriteRoomPersistsFullShape(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeRoomWritebackFixture(t, path)

	room := RoomWriteback{
		ID:          "chat",
		Name:        "General Chat",
		Visibility:  protocol.RoomVisibilityPublic,
		WritePolicy: protocol.RoomWritePolicyRegisteredAgents,
		Members:     []string{"alice", "bob"},
		Federation:  protocol.DefaultRoomFederation(),
		Credential:  "room-join-secret",
	}
	if err := WriteRoom(path, room); err != nil {
		t.Fatalf("WriteRoom() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if strings.Contains(string(raw), "[REDACTED]") {
		t.Fatalf("written config contains a redacted credential:\n%s", raw)
	}
	if !strings.Contains(string(raw), "room-join-secret") {
		t.Fatalf("written config does not contain the plaintext credential:\n%s", raw)
	}

	config, err := LoadConfigForPath(path, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Rooms) != 1 {
		t.Fatalf("expected exactly one room, got %#v", config.Rooms)
	}
	got := config.Rooms[0]
	if got.Name != "General Chat" ||
		got.Visibility != protocol.RoomVisibilityPublic ||
		got.WritePolicy != protocol.RoomWritePolicyRegisteredAgents ||
		len(got.Members) != 2 || got.Members[0] != "alice" || got.Members[1] != "bob" {
		t.Fatalf("unexpected persisted room %#v", got)
	}
	if got.Credential == nil || got.Credential.Token.Reveal() != "room-join-secret" {
		t.Fatalf("unexpected persisted credential %#v", got.Credential)
	}
}

// TestWriteRoomRefusesDuplicateID asserts a second WriteRoom call for an
// id already present in rooms[] fails outright — `room create` has no
// in-place-update story (config_writeback_room.go's own doc comment), so a
// repeat is always surfaced as a mistake, never a silent overwrite.
func TestWriteRoomRefusesDuplicateID(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeRoomWritebackFixture(t, path)

	room := RoomWriteback{ID: "chat", Federation: protocol.DefaultRoomFederation()}
	if err := WriteRoom(path, room); err != nil {
		t.Fatalf("first WriteRoom() error = %v", err)
	}
	if err := WriteRoom(path, room); !errors.Is(err, ErrRoomConfigExists) {
		t.Fatalf("second WriteRoom() error = %v, want ErrRoomConfigExists", err)
	}
}
