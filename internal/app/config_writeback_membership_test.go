package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeMembershipTestConfig(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

const membershipTestConfigBody = `version: moltnet.v1
network:
  id: chat-net
storage:
  kind: memory
rooms:
  - id: chat
    federation: none
    members: [alice]
pairings: []
`

func TestUpdateRoomMembersAddsAndDedupes(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeMembershipTestConfig(t, path, membershipTestConfigBody)

	if err := UpdateRoomMembers(path, "chat", []string{"bob", "alice"}, nil); err != nil {
		t.Fatalf("UpdateRoomMembers() error = %v", err)
	}

	config, err := LoadFile(path, "")
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	room := findRoomConfig(t, config, "chat")
	if got := room.Members; len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Fatalf("members = %#v, want [alice bob]", got)
	}
}

func TestUpdateRoomMembersRemovesAndCanEmptyList(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeMembershipTestConfig(t, path, membershipTestConfigBody)

	if err := UpdateRoomMembers(path, "chat", nil, []string{"alice"}); err != nil {
		t.Fatalf("UpdateRoomMembers() error = %v", err)
	}

	config, err := LoadFile(path, "")
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	room := findRoomConfig(t, config, "chat")
	if len(room.Members) != 0 {
		t.Fatalf("members = %#v, want empty", room.Members)
	}
}

// TestUpdateRoomMembersRemoveWinsOverAdd matches
// protocol.UpdateRoomMembersRequest's own semantics: an id present in both
// add and remove ends up removed, not added.
func TestUpdateRoomMembersRemoveWinsOverAdd(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeMembershipTestConfig(t, path, membershipTestConfigBody)

	if err := UpdateRoomMembers(path, "chat", []string{"carol"}, []string{"carol"}); err != nil {
		t.Fatalf("UpdateRoomMembers() error = %v", err)
	}

	config, err := LoadFile(path, "")
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	room := findRoomConfig(t, config, "chat")
	for _, member := range room.Members {
		if member == "carol" {
			t.Fatalf("members = %#v, want carol excluded", room.Members)
		}
	}
}

func TestUpdateRoomMembersUnknownRoomIsDistinguishableError(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeMembershipTestConfig(t, path, membershipTestConfigBody)

	err := UpdateRoomMembers(path, "does-not-exist", []string{"alice"}, nil)
	if !errors.Is(err, ErrRoomConfigNotFound) {
		t.Fatalf("UpdateRoomMembers() error = %v, want ErrRoomConfigNotFound", err)
	}
}

func TestUpdateRoomMembersRefusesSymlinkedConfig(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	realPath := filepath.Join(directory, "Moltnet.real")
	writeMembershipTestConfig(t, realPath, membershipTestConfigBody)
	linkPath := filepath.Join(directory, "Moltnet")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("Symlink(): %v", err)
	}

	if err := UpdateRoomMembers(linkPath, "chat", []string{"bob"}, nil); err == nil {
		t.Fatalf("UpdateRoomMembers() over a symlinked config error = nil, want refusal")
	}
}

func findRoomConfig(t *testing.T, config Config, id string) RoomConfig {
	t.Helper()
	for _, room := range config.Rooms {
		if room.ID == id {
			return room
		}
	}
	t.Fatalf("room %q not found in config, have %#v", id, config.Rooms)
	return RoomConfig{}
}
