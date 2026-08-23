package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// ErrRoomConfigExists indicates rooms[] in the target Moltnet config file
// already has an entry with the same id as the room WriteRoom was asked to
// create.
var ErrRoomConfigExists = errors.New("room already exists in config")

// RoomWriteback is the plaintext rooms[] entry `moltnet room create` appends
// to a Moltnet config file. Like PairingWriteback (config_writeback.go), it
// carries Credential as a plain string instead of protocol.SecretString: a
// typed Config/RoomConfig round-trip would marshal an existing room's
// credential as [REDACTED] (SecretString's MarshalJSON/MarshalYAML), so
// WriteRoom edits the document as an untyped map exactly like WritePairing
// does, and this type mirrors that same plaintext-preserving shape for
// rooms.
type RoomWriteback struct {
	ID          string
	Name        string
	Visibility  string
	WritePolicy string
	Members     []string
	Federation  *protocol.RoomFederation
	Credential  string
}

// WriteRoom appends room into the Moltnet config file at path, preserving
// every other plaintext secret already there. It refuses outright when
// rooms[] already has an entry with room.ID — unlike WritePairing's
// --force-overridable ErrPairingExists, `room create` has no in-place-update
// story yet (that is `admin room members add`/a future `room update`'s job),
// so a second `room create` for the same id is always a mistake to surface,
// never a silent overwrite. The write is atomic (temp file + rename in the
// same directory), mode 0600, and refuses a symlinked target — the same
// mechanism WritePairing uses, reused here rather than shared with it
// directly: a room create writes a much wider field set (name, visibility,
// write_policy, members, credential) than the {id, federation} shape
// WritePairing's own upsertSharedRoom knows how to extend (PLAN.md 7A.1 —
// upsertSharedRoom is the wrong helper for this precisely because of that
// narrower shape).
//
// room.Federation must never be nil: PLAN.md's 7A federation rule requires
// every config-time room to declare an explicit stance so
// validateRoomFederationStances never trips the first time a pairing is
// added. Callers that want the "no federation configured yet" default pass
// protocol.DefaultRoomFederation() explicitly rather than leaving this nil —
// WriteRoom itself does not substitute a default, so a nil here would write
// a room this same package's own federation rule then refuses to load.
func WriteRoom(path string, room RoomWriteback) error {
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

	if err := insertRoom(doc, room); err != nil {
		return err
	}

	out, err := encodeWritebackDocument(format, doc)
	if err != nil {
		return fmt.Errorf("encode Moltnet config %q: %w", path, err)
	}

	return atomicWriteConfigFile(path, out)
}

func insertRoom(doc map[string]any, room RoomWriteback) error {
	list := asMapSlice(doc["rooms"])
	for _, existing := range list {
		if stringField(existing, "id") == room.ID {
			return fmt.Errorf("%w: %q", ErrRoomConfigExists, room.ID)
		}
	}

	list = append(list, roomWritebackMap(room))
	doc["rooms"] = toAnySlice(list)
	return nil
}

func roomWritebackMap(room RoomWriteback) map[string]any {
	entry := map[string]any{
		"id":         room.ID,
		"federation": roomFederationWritebackValue(room.Federation),
	}
	if room.Name != "" {
		entry["name"] = room.Name
	}
	if len(room.Members) > 0 {
		entry["members"] = append([]string(nil), room.Members...)
	}
	if room.Visibility != "" {
		entry["visibility"] = room.Visibility
	}
	if room.WritePolicy != "" {
		entry["write_policy"] = room.WritePolicy
	}
	if strings.TrimSpace(room.Credential) != "" {
		entry["credential"] = map[string]any{"token": room.Credential}
	}
	return entry
}

// roomFederationWritebackValue mirrors protocol.RoomFederation's own
// MarshalJSON/MarshalYAML encoding (federation.go) without routing a
// SecretString-adjacent typed struct through the untyped document: "none" or
// "all" scalars, or the list of pairing ids for "list" mode. A nil
// federation normalizes to "none" (protocol.NormalizeRoomFederation) — see
// WriteRoom's doc comment for why a room create writeback is never allowed
// to leave this field absent instead.
func roomFederationWritebackValue(federation *protocol.RoomFederation) any {
	normalized := protocol.NormalizeRoomFederation(federation)
	if normalized.Mode == protocol.RoomFederationList {
		return append([]string(nil), normalized.Pairings...)
	}
	return normalized.Mode
}
