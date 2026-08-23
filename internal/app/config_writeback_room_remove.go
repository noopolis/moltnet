package app

import (
	"fmt"
	"os"
	"strings"
)

// RemoveRoomFromConfig deletes the rooms[] entry with the given id from the
// Moltnet config file at path, preserving every other plaintext secret
// already there.
//
// P0-1 (confirmed live): store.RemoveRoomContext (internal/store/sql_store.go)
// is a soft delete -- it tombstones the row (deleted_at) but keeps its
// primary key. `room remove` used to only call DELETE /v1/rooms/{id}
// against the running service; the config's own rooms[] entry for that room
// survived untouched. On the next restart, applyRequestFromConfig
// (internal/app/app.go) replays every config-declared room:
// CreateRoomContext's plain INSERT hits the tombstoned primary key and
// fails with ErrRoomExists, so the apply path falls back to
// ReconcileRoomContext -- which never finds a live row for a soft-deleted
// id and returns not-found. The network fails to start. This was
// pre-existing for any hand-written config room, but `room create` (this
// same phase) started writing rooms into config, so it now applies to
// every room that command creates.
//
// The fix mirrors UpdateRoomMembers (config_writeback_membership.go): same
// document-editing shape, same ErrRoomConfigNotFound for "this room was
// never declared here" (created directly via the API, the console, or
// `moltnet apply`, none of which touch rooms[] -- nothing to remove, and no
// restart hazard either, since applyRequestFromConfig only replays rooms
// this file actually lists). The write is atomic (temp file + rename),
// mode 0600, and refuses a symlinked target, matching every other config
// writeback in this package.
func RemoveRoomFromConfig(path string, roomID string) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return fmt.Errorf("room id is required")
	}

	if err := rejectSymlinkedConfigPath(path); err != nil {
		return err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Moltnet config %q: %w", path, err)
	}

	format := configFormat(path)
	doc, err := decodeWritebackDocument(format, contents)
	if err != nil {
		return fmt.Errorf("decode Moltnet config %q: %w", path, err)
	}

	if err := removeRoomFromDoc(doc, roomID); err != nil {
		return err
	}

	out, err := encodeWritebackDocument(format, doc)
	if err != nil {
		return fmt.Errorf("encode Moltnet config %q: %w", path, err)
	}

	return atomicWriteConfigFile(path, out)
}

// removeRoomFromDoc deletes the rooms[] entry matching roomID in place,
// returning ErrRoomConfigNotFound (shared with UpdateRoomMembers) when
// rooms[] has no such entry.
func removeRoomFromDoc(doc map[string]any, roomID string) error {
	list := asMapSlice(doc["rooms"])
	for index, existing := range list {
		if stringField(existing, "id") != roomID {
			continue
		}
		list = append(list[:index], list[index+1:]...)
		doc["rooms"] = toAnySlice(list)
		return nil
	}
	return fmt.Errorf("%w: %q", ErrRoomConfigNotFound, roomID)
}
