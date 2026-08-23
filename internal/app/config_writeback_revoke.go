package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// ErrPairingNotFound indicates pairings[] in the target Moltnet config file
// has no entry with the given id -- RevokePairing has nothing to revoke.
var ErrPairingNotFound = errors.New("pairing not found")

// RevokePairing removes pairingID from pairings[], removes the matching
// auth.tokens[] entry, and strips pairingID out of every room's federation
// list in the Moltnet config file at path. It is WritePairing's inverse:
// `pair invite`/`pair <code>` write the peer's inbound credential into
// auth.tokens[] under the exact same id as the pairing (config_writeback.go's
// PairingWriteback/AuthTokenWriteback), so a revoke that only removed the
// pairings[] entry and left that token behind would leave the peer fully
// authenticated on a pairing that no longer appears anywhere in pairings[] —
// UX.md and PLAN.md's 7B.3 both call this out explicitly as the part that
// must not be skipped.
//
// Decision (7B.3's open question, since the plan asked for one and a
// reason): revoke DOES strip the room federation grant too, not just the
// pairing and its token. Leaving a room's federation list naming a revoked
// pairing id is a live grant waiting to be silently reactivated: generating
// a *new* pairing that happens to reuse the same id (`pair invite --id
// <id>` again) mints a fresh credential and, with the old federation entry
// untouched, instantly regains access to every room it names — no separate
// re-grant step, no visible signal the old grant survived. That makes
// revocation reversible by re-pairing alone, which defeats the point of a
// revoke command: an operator revoking access wants it gone until they
// deliberately grant it again. Stripping the federation entry means
// re-pairing under the same id starts from zero room access, exactly like a
// brand new pairing would; getting the old rooms back requires saying so
// again (`pair invite --room <id>` or `room create --federation`).
//
// A federation list that becomes empty after stripping reverts to the scalar
// "none": protocol.ValidateRoomFederation rejects an explicit "list" naming
// zero pairings outright ("federation list must name at least one pairing"),
// so leaving an emptied list in place would make the room's own config
// un-reloadable — caught immediately by this call's post-write
// LoadConfigForPath check (via the caller, revokePairingWithRollback), which
// would then roll the whole revoke back. "none" is the correct steady state
// for a room with no remaining federation grants regardless: it is exactly
// what protocol.DefaultRoomFederation returns for a room that was never
// federated to begin with. Only "all" federation is left untouched, matching
// upsertSharedRoom's own asymmetric handling of that case
// (federatedRoomFederationValue) — there is nothing to strip from a room
// that federates with everyone.
//
// RevokeResult reports what RevokePairing actually found and removed, so a
// caller (cmd/moltnet/pair_revoke.go) can describe the real outcome instead
// of assuming every leg it tries to strip was actually present. Namely: a
// pairing whose auth.tokens[] entry uses a different id than the pairing
// itself (an operator hand-edited config, most plausibly, since every id
// `pair invite`/`pair <code>` ever writes matches by construction) leaves
// the peer's credential live even after a "successful" revoke, and
// TokenRemoved is how the caller learns that happened instead of printing
// the same "credential removed" line regardless.
type RevokeResult struct {
	TokenRemoved bool
}

// Like WritePairing, this refuses a symlinked config target and writes
// atomically (temp file + rename), mode 0600. It does not itself verify the
// result reloads cleanly — callers wanting that (and a rollback on failure)
// use the same LoadConfigForPath-and-restore pattern writePairingWithRollback
// (cmd/moltnet/pair.go) already establishes for WritePairing.
func RevokePairing(path string, pairingID string) (RevokeResult, error) {
	pairingID = strings.TrimSpace(pairingID)
	if pairingID == "" {
		return RevokeResult{}, fmt.Errorf("pairing id is required")
	}

	if err := rejectSymlinkedConfigPath(path); err != nil {
		return RevokeResult{}, err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return RevokeResult{}, fmt.Errorf("read Moltnet config %q: %w", path, err)
	}

	format := configFormat(path)
	doc, err := decodeWritebackDocument(format, contents)
	if err != nil {
		return RevokeResult{}, fmt.Errorf("decode Moltnet config %q: %w", path, err)
	}

	if err := removePairing(doc, pairingID); err != nil {
		return RevokeResult{}, err
	}
	tokenRemoved := removeAuthToken(doc, pairingID)
	stripPairingFromRoomFederations(doc, pairingID)

	out, err := encodeWritebackDocument(format, doc)
	if err != nil {
		return RevokeResult{}, fmt.Errorf("encode Moltnet config %q: %w", path, err)
	}

	if err := atomicWriteConfigFile(path, out); err != nil {
		return RevokeResult{}, err
	}
	return RevokeResult{TokenRemoved: tokenRemoved}, nil
}

// removePairing deletes the pairings[] entry with id pairingID, reporting
// ErrPairingNotFound when there is none — a revoke against an id that was
// never paired (typo, already revoked) is a caller mistake worth surfacing,
// unlike removeAuthToken's and stripPairingFromRoomFederations's silent
// no-ops below, which cover already-legitimately-absent state (a pairing
// invited before auth.tokens[] existed, or one whose rooms were never
// federated to it).
func removePairing(doc map[string]any, pairingID string) error {
	list := asMapSlice(doc["pairings"])
	filtered := make([]map[string]any, 0, len(list))
	found := false
	for _, existing := range list {
		if stringField(existing, "id") == pairingID {
			found = true
			continue
		}
		filtered = append(filtered, existing)
	}
	if !found {
		return fmt.Errorf("%w: %q", ErrPairingNotFound, pairingID)
	}
	doc["pairings"] = toAnySlice(filtered)
	return nil
}

// removeAuthToken deletes the auth.tokens[] entry with id tokenID, reporting
// whether one was actually found and removed -- RevokeResult.TokenRemoved
// surfaces that to the operator instead of a revoke silently claiming a
// credential was cut off when its id never matched anything. A config with
// no auth.tokens[] at all (or no "auth" section) is left completely
// untouched: writing back an empty `tokens: []` into a config that never had
// a tokens key would be a needless, unrelated diff.
func removeAuthToken(doc map[string]any, tokenID string) bool {
	auth, _ := doc["auth"].(map[string]any)
	if auth == nil {
		return false
	}
	if _, hasTokens := auth["tokens"]; !hasTokens {
		return false
	}
	list := asMapSlice(auth["tokens"])
	filtered := make([]map[string]any, 0, len(list))
	found := false
	for _, existing := range list {
		if stringField(existing, "id") == tokenID {
			found = true
			continue
		}
		filtered = append(filtered, existing)
	}
	if !found {
		return false
	}
	auth["tokens"] = toAnySlice(filtered)
	doc["auth"] = auth
	return true
}

func stripPairingFromRoomFederations(doc map[string]any, pairingID string) {
	list := asMapSlice(doc["rooms"])
	changedAny := false
	for index, existing := range list {
		if federation, changed := stripPairingFromFederationValue(existing["federation"], pairingID); changed {
			existing["federation"] = federation
			list[index] = existing
			changedAny = true
		}
	}
	if changedAny {
		doc["rooms"] = toAnySlice(list)
	}
}

// stripPairingFromFederationValue is federatedRoomFederationValue's inverse:
// a list loses pairingID if present, reverting to the scalar "none" if that
// empties it (protocol.ValidateRoomFederation rejects a zero-length list —
// see RevokePairing's doc comment); "all", "none", an absent field, or any
// other scalar this package does not model as a list are left untouched
// (nothing to strip from them). changed reports whether current actually
// needed rewriting.
func stripPairingFromFederationValue(current any, pairingID string) (any, bool) {
	items, ok := current.([]any)
	if !ok {
		return current, false
	}

	filtered := make([]string, 0, len(items))
	changed := false
	for _, item := range items {
		id, _ := item.(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if id == pairingID {
			changed = true
			continue
		}
		filtered = append(filtered, id)
	}
	if !changed {
		return current, false
	}
	if len(filtered) == 0 {
		return protocol.RoomFederationNone, true
	}
	return filtered, true
}
