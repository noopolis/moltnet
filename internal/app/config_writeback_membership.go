package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// ErrRoomConfigNotFound indicates rooms[] in the target Moltnet config file
// has no entry with the given id, so UpdateRoomMembers has nothing to
// update. Unlike ErrRoomConfigExists (config_writeback_room.go), this is not
// necessarily a caller mistake: a room can exist on the live service without
// ever having been declared in this config file (created directly via
// POST /v1/rooms, the console, or `moltnet apply`, none of which touch
// rooms[]) — see UpdateRoomMembers's doc comment for how callers are meant
// to treat this case.
var ErrRoomConfigNotFound = errors.New("room not found in config")

// UpdateRoomMembers applies add/remove membership changes to an existing
// rooms[] entry's members list in the Moltnet config file at path.
//
// PLAN.md 7A.1 named the bug this closes: `admin room members add` PATCHes
// the live API only, but startup's applyRequestFromConfig (app.go) replaces
// every config-declared room's member list wholesale via
// ReconcileRoomContext, so membership granted only through the API is
// silently undone at the next restart -- reproduced live as "chat members =
// [alice] before restart, nil after". `admin room members add` is also
// named as the pairing-grant aftercare step in the same plan, so the two
// mechanisms were fighting each other. This gives membership one durable
// source of truth: the API call still updates the live service immediately
// (no restart needed to see the effect), and this writeback keeps the config
// file in sync so the *next* restart reconciles to the same state instead of
// wiping it.
//
// A caller whose room id is not in rooms[] at all -- see ErrRoomConfigNotFound
// -- should treat that as "this room's membership cannot survive a restart
// yet" and warn, not fail: the live API mutation this writeback follows has
// already succeeded by the time this runs, and a config file that never
// declared the room in the first place is a pre-existing condition this
// single call cannot repair (the room itself would need `room create` or
// `pair invite --room` first).
//
// add and remove are both normalized with protocol.UniqueTrimmedStrings, add
// is applied first (removal always wins when the same id appears in both,
// matching protocol.UpdateRoomMembersRequest's own semantics), and both are
// safe to pass empty. The write is atomic (temp file + rename), mode 0600,
// and refuses a symlinked target -- the same invariant every other config
// writeback in this package holds.
func UpdateRoomMembers(path string, roomID string, add []string, remove []string) error {
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

	if err := updateRoomMembersInDoc(doc, roomID, add, remove); err != nil {
		return err
	}

	out, err := encodeWritebackDocument(format, doc)
	if err != nil {
		return fmt.Errorf("encode Moltnet config %q: %w", path, err)
	}

	return atomicWriteConfigFile(path, out)
}

func updateRoomMembersInDoc(doc map[string]any, roomID string, add []string, remove []string) error {
	list := asMapSlice(doc["rooms"])
	for index, existing := range list {
		if stringField(existing, "id") != roomID {
			continue
		}

		members := protocol.UniqueTrimmedStrings(append(stringSliceField(existing, "members"), add...))
		members = subtractStrings(members, remove)
		if len(members) > 0 {
			existing["members"] = toAnyStringSlice(members)
		} else {
			delete(existing, "members")
		}

		list[index] = existing
		doc["rooms"] = toAnySlice(list)
		return nil
	}
	return fmt.Errorf("%w: %q", ErrRoomConfigNotFound, roomID)
}

// stringSliceField reads key off entry as a []string, skipping any element
// that is not a plain string (an untyped YAML/JSON decode of a well-formed
// members list never produces anything else, but this stays defensive
// rather than panicking on a hand-edited config).
func stringSliceField(entry map[string]any, key string) []string {
	items, _ := entry[key].([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			result = append(result, value)
		}
	}
	return result
}

// subtractStrings returns values with every entry named in remove excluded,
// after normalizing remove the same way the rest of this file normalizes
// id lists (trim, dedupe, drop blanks).
func subtractStrings(values []string, remove []string) []string {
	excluded := make(map[string]struct{}, len(remove))
	for _, value := range protocol.UniqueTrimmedStrings(remove) {
		excluded[value] = struct{}{}
	}
	if len(excluded) == 0 {
		return values
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, skip := excluded[value]; !skip {
			result = append(result, value)
		}
	}
	return result
}

func toAnyStringSlice(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
