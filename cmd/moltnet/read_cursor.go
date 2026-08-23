package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

// PLAN.md 7C.2, open question 3 (DECIDED): the `read --since-last` cursor is
// per-identity and always lives under `.moltnet/`. An agent keys it by
// network id + member id (both required, non-empty fields on a resolved
// AttachmentConfig); the zero-setup operator fallback has no member id at
// all, so it keys by network id alone, and its cursor file sits beside the
// local server's own config instead of a client config. One storage
// location's file *format* (readCursorFile, below), two key derivations
// (resolveReadCursorIdentity) -- never a path where one identity's entry can
// be read or advanced as another's.

// readCursorVersionV1 stamps the on-disk cursor file format. Additive-only:
// a future field lands as a new optional JSON key, never a meaning change to
// an existing one, so this constant only needs to move on an actual breaking
// change.
const readCursorVersionV1 = "moltnet.read-cursor.v1"

// readCursorFileName is the one on-disk file every identity's entries live
// inside (see this file's package doc comment above), sitting beside
// whichever Moltnet config resolveReadCursorIdentity anchored to -- the same
// directory register-agent already writes identity.json into, for the agent
// path.
const readCursorFileName = "read-cursor.json"

type readCursorFile struct {
	Version string                     `json:"version"`
	Entries map[string]readCursorEntry `json:"entries"`
}

type readCursorEntry struct {
	LastMessageID string    `json:"last_message_id"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// readCursorIdentity is resolveReadCursorIdentity's result: where this
// identity's cursor file lives (path), and the key prefix that scopes every
// entry this identity ever writes into it. Because keyPrefix already
// disambiguates identity, the same file can safely hold entries for several
// different identities (see readCursorKey) without any of them colliding.
type readCursorIdentity struct {
	path      string
	keyPrefix string
}

// resolveReadCursorIdentity implements PLAN.md 7C.2's binding storage
// decision:
//
//   - The agent (client-config) path keys its cursor by network id + member
//     id -- both already required, non-empty fields on a resolved
//     AttachmentConfig (clientconfig.AttachmentConfig.Validate) -- and its
//     cursor file sits in the same directory clientconfig.DiscoverPath
//     resolved the client config.json from, exactly where register-agent
//     already writes identity.json.
//   - The zero-setup operator fallback has no member id at all
//     (client_operator_fallback.go's resolveOperatorClient never sets one),
//     so it keys by network id only, and its cursor file sits beside the
//     local server's own resolved config -- the same directory
//     storage.sqlite.path's default (".moltnet/moltnet.db",
//     internal/app/config.go) already anchors to
//     (internal/app/config_load.go's anchorStoragePaths), i.e. the
//     ".moltnet/" directory PLAN.md's open question 3 names explicitly.
//
// These are two different directories in the common case (an agent's own
// workspace vs. the server's own config directory). Even in the degenerate
// case where they happen to coincide -- an operator running `read` from the
// very directory their own server's config lives in -- the two key prefixes
// ("agent:<network>:<member>" vs "operator:<network>") can never collide,
// since only the agent prefix ever carries a member id and member ids are
// never empty (clientconfig validates this).
//
// The raw --base-url override (bindOperatorOverrideFlags) is refused
// outright for --since-last: it names no local Moltnet config at all, not
// even the local server's, so there is nowhere on this machine to durably
// anchor a cursor file to.
func resolveReadCursorIdentity(
	configPath string,
	networkFlag string,
	attachment clientconfig.AttachmentConfig,
	usingFallback bool,
	baseURLOverrideGiven bool,
) (readCursorIdentity, error) {
	if baseURLOverrideGiven {
		return readCursorIdentity{}, fmt.Errorf(
			"--since-last needs a locally resolvable Moltnet config to durably store its cursor in; it does not support --base-url overrides -- drop --base-url, or use --config/--network instead",
		)
	}

	if usingFallback {
		serverConfigPath, found, err := app.ResolveConfigPath("", strings.TrimSpace(networkFlag))
		if err != nil {
			return readCursorIdentity{}, err
		}
		if !found {
			return readCursorIdentity{}, fmt.Errorf("--since-last could not resolve a local Moltnet server config; run `moltnet init` first")
		}
		return readCursorIdentity{
			path:      filepath.Join(filepath.Dir(serverConfigPath), readCursorFileName),
			keyPrefix: "operator:" + url.QueryEscape(attachment.NetworkID),
		}, nil
	}

	discovered, ok, err := clientconfig.DiscoverPath(configPath)
	if err != nil {
		return readCursorIdentity{}, err
	}
	if !ok {
		return readCursorIdentity{}, fmt.Errorf("--since-last could not locate the Moltnet client config directory")
	}
	return readCursorIdentity{
		path:      filepath.Join(filepath.Dir(discovered), readCursorFileName),
		keyPrefix: "agent:" + url.QueryEscape(attachment.NetworkID) + ":" + url.QueryEscape(attachment.MemberID),
	}, nil
}

// readCursorKey derives the composite key one (identity, target) pair owns
// inside the shared cursor file. identity.keyPrefix already scopes out every
// other identity that might share the file; appending the target's own kind
// and id additionally keeps two different rooms/threads/DMs the same
// identity reads from independent of one another -- message ids are only
// ever meaningful within their own target's stream, so a cursor shared
// across targets would silently misbehave (either an immediate "invalid
// cursor" against the wrong stream, or a coincidentally-valid but wrong
// resume point).
//
// target.id is url.QueryEscape'd before joining (identity.keyPrefix's own
// variable components -- network id and member id -- are already escaped by
// resolveReadCursorIdentity for the same reason): network id, member id, and
// room/thread/DM ids are all validated against
// pkg/protocol/validate.go's ValidateMessageID pattern
// ([A-Za-z0-9][A-Za-z0-9._:-]{0,127}), which legally allows ":" -- and
// ValidateMemberID additionally blesses scoped "network:agent" member ids
// carrying their own colons. Joining raw, unescaped values with ":" would
// let two different (identity, target) tuples produce the same string (e.g.
// member "bot" reading room "q:room:z" vs. member "bot:room:q" reading room
// "z"), silently sharing one cursor entry. QueryEscape maps every ":" (and
// every other byte outside its unreserved set) to a "%XX" triplet that
// itself contains no literal ":", so the ":" characters joining the
// escaped components below can never be confused with one that was part of
// an original value -- making this key injective in (keyPrefix, target.kind,
// target.id).
func readCursorKey(identity readCursorIdentity, target targetRef) string {
	return identity.keyPrefix + ":" + target.kind + ":" + url.QueryEscape(target.id)
}

// loadReadCursorFile reads path's cursor file, or returns a fresh empty one
// (never an error) when it does not exist yet -- every identity's very first
// --since-last use.
func loadReadCursorFile(path string) (readCursorFile, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return readCursorFile{Version: readCursorVersionV1, Entries: map[string]readCursorEntry{}}, nil
		}
		return readCursorFile{}, fmt.Errorf("read Moltnet read-cursor file %q: %w", path, err)
	}

	var file readCursorFile
	if err := json.Unmarshal(contents, &file); err != nil {
		return readCursorFile{}, fmt.Errorf("decode Moltnet read-cursor file %q: %w", path, err)
	}
	if file.Entries == nil {
		file.Entries = map[string]readCursorEntry{}
	}
	if strings.TrimSpace(file.Version) == "" {
		file.Version = readCursorVersionV1
	}
	return file, nil
}

// lookupReadCursor returns the stored message id for key, or "" when this is
// the identity's first --since-last use against this target. PLAN.md 7C.2's
// documented first-use behavior: an empty return means no
// PageRequest.After filter gets applied, so the very first --since-last read
// is a plain, unfiltered read (the newest --limit messages) -- identical to
// `read` without --since-last -- rather than silently swallowing pre-existing
// history or erroring for lack of a baseline.
func lookupReadCursor(path string, key string) (string, error) {
	file, err := loadReadCursorFile(path)
	if err != nil {
		return "", err
	}
	return file.Entries[key].LastMessageID, nil
}

// advanceReadCursor persists messageID as key's cursor, read-modify-write
// over the whole file so every other key's entry -- every other identity or
// target that happens to share this same file -- survives untouched. Callers
// (runRead) only ever reach this after a successful fetch: PLAN.md 7C.2's
// documented "the cursor advances on success only, never on error" rule.
func advanceReadCursor(path string, key string, messageID string) error {
	file, err := loadReadCursorFile(path)
	if err != nil {
		return err
	}
	file.Entries[key] = readCursorEntry{LastMessageID: messageID, UpdatedAt: time.Now().UTC()}
	return writeReadCursorFile(path, file)
}

// writeReadCursorFile writes file atomically (temp file + rename, 0600) --
// the same pattern writeClientConfig (client_helpers.go) uses for the client
// config this file sits beside, so a crash or a concurrent reader never
// observes a half-written cursor file.
func writeReadCursorFile(path string, file readCursorFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create Moltnet read-cursor directory: %w", err)
	}

	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Moltnet read-cursor file: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".read-cursor-*.json")
	if err != nil {
		return fmt.Errorf("create temporary Moltnet read-cursor file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temporary Moltnet read-cursor file: %w", err)
	}
	if _, err := temp.Write(append(payload, '\n')); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary Moltnet read-cursor file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary Moltnet read-cursor file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace Moltnet read-cursor file: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// isInvalidCursorError reports whether err is the "invalid cursor" 422
// internal/rooms/errors.go's invalidCursorReasonError produces -- both for a
// cursor whose message id no longer exists (PLAN.md 7C.2's "cursor older
// than retained history" case) and for a structurally malformed one
// (protocol.ValidatePageRequest's ValidateMessageID check, wired through the
// same ErrInvalidCursor path in internal/store/sql_query.go). The CLI's
// internal/client has no structured error type today -- doJSON only ever
// returns the response's raw text wrapped in a generic error -- so this is a
// pragmatic substring match against that fixed, single-source message,
// deliberately scoped to this one call site rather than generalized into
// internal/client itself.
func isInvalidCursorError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "invalid cursor")
}
