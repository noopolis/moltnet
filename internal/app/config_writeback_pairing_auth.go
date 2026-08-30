package app

import (
	"fmt"
	"strings"
)

// rejectAuthTokenIDConflict refuses tokenID when auth.tokens[] already has
// an entry with that exact id that is not a plain pair-scoped credential
// (see ErrAuthTokenIDConflict's doc comment in config_writeback.go for why).
// A pre-existing entry that is itself scoped to exactly [pair] is left
// alone -- that is the legitimate re-invite/rejoin case (WritePairing
// overwriting its own prior pairing's token, e.g. `pair invite --id <id>
// --force` reusing an id), not a collision with an unrelated credential.
func rejectAuthTokenIDConflict(doc map[string]any, tokenID string) error {
	auth, _ := doc["auth"].(map[string]any)
	if auth == nil {
		return nil
	}
	for _, existing := range asMapSlice(auth["tokens"]) {
		if stringField(existing, "id") != tokenID {
			continue
		}
		if tokenEntryIsPairScopedOnly(existing) {
			return nil
		}
		return fmt.Errorf("%w: %q already names a non-pair auth token; choose a different pairing id", ErrAuthTokenIDConflict, tokenID)
	}
	return nil
}

// tokenEntryIsPairScopedOnly reports whether a decoded auth.tokens[] entry's
// scopes are exactly ["pair"] and nothing else -- an operator credential
// (any of observe/write/admin/attach) never matches, regardless of whether
// it also happens to carry "pair" (see internal/auth's
// authn.Claims.Operator for the same operator-shaped-credential concept
// used at enforcement time).
func tokenEntryIsPairScopedOnly(entry map[string]any) bool {
	scopes, _ := entry["scopes"].([]any)
	if len(scopes) == 0 {
		return false
	}
	for _, scope := range scopes {
		value, _ := scope.(string)
		if strings.TrimSpace(value) != "pair" {
			return false
		}
	}
	return true
}

func upsertAuthToken(doc map[string]any, token AuthTokenWriteback) {
	auth, _ := doc["auth"].(map[string]any)
	if auth == nil {
		auth = map[string]any{}
	}

	list := asMapSlice(auth["tokens"])
	entry := authTokenWritebackMap(token)

	for index, existing := range list {
		if stringField(existing, "id") == token.ID {
			list[index] = entry
			auth["tokens"] = toAnySlice(list)
			doc["auth"] = auth
			return
		}
	}

	list = append(list, entry)
	auth["tokens"] = toAnySlice(list)
	doc["auth"] = auth
}

func authTokenWritebackMap(token AuthTokenWriteback) map[string]any {
	entry := map[string]any{
		"id":     token.ID,
		"value":  token.Value,
		"scopes": append([]string(nil), token.Scopes...),
	}
	if token.Network != "" {
		entry["network"] = token.Network
	}
	return entry
}
