package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
	"go.yaml.in/yaml/v3"
)

// ErrPairingExists indicates a pairings[] entry with the same id already
// exists in the target Moltnet config file.
var ErrPairingExists = errors.New("pairing already exists")

// ErrAuthTokenIDConflict indicates a pairing's id collides with an existing
// auth.tokens[] entry that is not a plain pair-scoped credential.
//
// F5 (confirmed): the pairing id that ends up as AuthTokenWriteback.ID is
// invite-controlled on both sides of a pairing -- `pair invite --id <id>`
// picks it directly, and `pair <code>` takes whatever pairing_id the invite
// names, with no round trip to confirm it first. upsertAuthToken
// (below) overwrites-by-id unconditionally, so an invite (or a hand-crafted
// one) naming an id that collides with an operator's own static token --
// most dangerously "operator" itself -- used to silently replace that
// token's value and scopes with a bare [pair]-scoped credential, locking the
// real operator out of their own network. This is refused even under
// --force: --force is documented and tested as "overwrite an existing
// *pairing* with this id", never "overwrite an unrelated non-pair
// credential" -- those are different invariants and only the first one is
// what --force opts into.
var ErrAuthTokenIDConflict = errors.New("pairing id collides with a non-pair auth token")

// PairingWriteback is the plaintext pairings[] entry a pair command appends
// to a Moltnet config file. It uses plain strings instead of
// protocol.SecretString on purpose: SecretString marshals as [REDACTED], so
// round-tripping the loaded config structs through YAML/JSON would corrupt
// every secret already in the file. WritePairing instead edits the document
// as an untyped map, so no SecretString ever enters the write path.
type PairingWriteback struct {
	ID                string
	RemoteNetworkID   string
	RemoteNetworkName string
	Token             string
	Relay             *PairingRelayWriteback
}

// PairingRelayWriteback is the plaintext pairings[].relay entry.
type PairingRelayWriteback struct {
	URL   string
	Room  string
	Token string
}

// AuthTokenWriteback is the plaintext pair-scoped auth.tokens[] entry a pair
// command appends alongside the pairing.
type AuthTokenWriteback struct {
	ID      string
	Value   string
	Network string
	Scopes  []string
}

// WritePairing appends pairing and authToken into the Moltnet config file at
// path, preserving every other plaintext secret already there. It refuses to
// overwrite an existing pairings[] entry with the same id unless force is
// true. The write is atomic (temp file + rename in the same directory), mode
// 0600, and refuses a symlinked target, matching the server's private-config
// invariant for files that hold inline secrets.
//
// When roomIDs is non-empty, each named room is created in rooms[] if
// absent, and its federation is extended to allow pairing.ID: an absent or
// "none" federation becomes a list naming just this pairing, an existing
// list gets this pairing appended (idempotently — re-running never
// duplicates the entry), and an existing "all" federation is left alone.
// This is the same single atomic write and post-write reload check as the
// pairing/auth-token writeback, so a room upsert never lands without the
// pairing it exists for, or vice versa.
func WritePairing(path string, pairing PairingWriteback, authToken AuthTokenWriteback, roomIDs []string, force bool) error {
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

	if err := validateSharedRoomIDs(roomIDs); err != nil {
		return err
	}

	if err := upsertPairing(doc, pairing, force); err != nil {
		return err
	}
	if err := rejectAuthTokenIDConflict(doc, authToken.ID); err != nil {
		return err
	}
	upsertAuthToken(doc, authToken)
	upsertSharedRooms(doc, roomIDs, pairing.ID)

	out, err := encodeWritebackDocument(format, doc)
	if err != nil {
		return fmt.Errorf("encode Moltnet config %q: %w", path, err)
	}

	return atomicWriteConfigFile(path, out)
}

func rejectSymlinkedConfigPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat Moltnet config %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("moltnet config %q must not be a symlink", path)
	}
	return nil
}

func decodeWritebackDocument(format string, contents []byte) (map[string]any, error) {
	doc := map[string]any{}
	if len(bytes.TrimSpace(contents)) == 0 {
		return doc, nil
	}

	var err error
	if format == "json" {
		err = json.Unmarshal(contents, &doc)
	} else {
		err = yaml.Unmarshal(contents, &doc)
	}
	if err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func encodeWritebackDocument(format string, doc map[string]any) ([]byte, error) {
	if format == "json" {
		return json.MarshalIndent(doc, "", "  ")
	}
	return yaml.Marshal(doc)
}

func upsertPairing(doc map[string]any, pairing PairingWriteback, force bool) error {
	list := asMapSlice(doc["pairings"])
	entry := pairingWritebackMap(pairing)

	for index, existing := range list {
		if stringField(existing, "id") == pairing.ID {
			if !force {
				return fmt.Errorf("%w: %q (rerun with --force to overwrite)", ErrPairingExists, pairing.ID)
			}
			list[index] = entry
			doc["pairings"] = toAnySlice(list)
			return nil
		}
	}

	list = append(list, entry)
	doc["pairings"] = toAnySlice(list)
	return nil
}

// validateSharedRoomIDs rejects any non-blank id in roomIDs that would fail
// protocol.ValidateRoomID, before WritePairing touches the config document.
// A room id that fails this check can never receive a relayed message (the
// same rule is enforced at send time), so writing it would create a room
// that is permanently dead rather than surfacing the mistake up front.
func validateSharedRoomIDs(roomIDs []string) error {
	for _, roomID := range protocol.UniqueTrimmedStrings(roomIDs) {
		if err := protocol.ValidateRoomID(roomID); err != nil {
			return fmt.Errorf("--room %q is invalid: %w", roomID, err)
		}
	}
	return nil
}

// upsertSharedRooms ensures every id in roomIDs exists in doc's rooms[] and
// that its federation allows pairingID, creating rooms that are absent.
// Blank ids and duplicates (after trimming) are ignored so callers can pass
// an invite's shared_rooms list unfiltered.
func upsertSharedRooms(doc map[string]any, roomIDs []string, pairingID string) {
	for _, roomID := range protocol.UniqueTrimmedStrings(roomIDs) {
		upsertSharedRoom(doc, roomID, pairingID)
	}
}

func upsertSharedRoom(doc map[string]any, roomID string, pairingID string) {
	list := asMapSlice(doc["rooms"])

	for index, existing := range list {
		if stringField(existing, "id") == roomID {
			existing["federation"] = federatedRoomFederationValue(existing["federation"], pairingID)
			list[index] = existing
			doc["rooms"] = toAnySlice(list)
			return
		}
	}

	list = append(list, map[string]any{
		"id":         roomID,
		"federation": []string{pairingID},
	})
	doc["rooms"] = toAnySlice(list)
}

// federatedRoomFederationValue computes the rooms[].federation value that
// should replace current so it allows pairingID:
//   - absent, empty, or "none" becomes a list naming only pairingID
//   - "all" is left alone (it already allows every pairing)
//   - an existing list gets pairingID appended, unless already present
//
// Any other scalar (a federation mode this package does not know about) is
// treated like "none": WritePairing's caller-driven post-write config reload
// is the backstop that rejects genuinely invalid federation values.
func federatedRoomFederationValue(current any, pairingID string) any {
	switch value := current.(type) {
	case nil:
		return []string{pairingID}
	case string:
		if strings.TrimSpace(value) == protocol.RoomFederationAll {
			return value
		}
		return []string{pairingID}
	case []any:
		pairings := make([]string, 0, len(value)+1)
		found := false
		for _, item := range value {
			id, _ := item.(string)
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			pairings = append(pairings, id)
			if id == pairingID {
				found = true
			}
		}
		if !found {
			pairings = append(pairings, pairingID)
		}
		return pairings
	default:
		return []string{pairingID}
	}
}

func pairingWritebackMap(pairing PairingWriteback) map[string]any {
	entry := map[string]any{
		"id":                pairing.ID,
		"remote_network_id": pairing.RemoteNetworkID,
		"token":             pairing.Token,
	}
	if pairing.RemoteNetworkName != "" {
		entry["remote_network_name"] = pairing.RemoteNetworkName
	}
	if pairing.Relay != nil {
		entry["relay"] = map[string]any{
			"url":   pairing.Relay.URL,
			"room":  pairing.Relay.Room,
			"token": pairing.Relay.Token,
		}
	}
	return entry
}

func asMapSlice(value any) []map[string]any {
	items, _ := value.([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if entry, ok := item.(map[string]any); ok {
			result = append(result, entry)
		}
	}
	return result
}

func toAnySlice(items []map[string]any) []any {
	result := make([]any, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	return result
}

func stringField(entry map[string]any, key string) string {
	value, _ := entry[key].(string)
	return value
}

func atomicWriteConfigFile(path string, contents []byte) error {
	dir := filepath.Dir(path)

	temp, err := os.CreateTemp(dir, ".moltnet-config-*")
	if err != nil {
		return fmt.Errorf("create temporary Moltnet config: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temporary Moltnet config: %w", err)
	}
	if _, err := temp.Write(contents); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary Moltnet config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary Moltnet config: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace Moltnet config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod Moltnet config: %w", err)
	}

	return nil
}
