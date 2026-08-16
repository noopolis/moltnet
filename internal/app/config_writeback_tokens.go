package app

import (
	"errors"
	"fmt"
	"os"
)

// ErrAuthTokensExist indicates auth.tokens[] in the target Moltnet config
// already has at least one entry, so AddOperatorToken refuses rather than
// risk creating an unwanted second admin-scoped credential.
var ErrAuthTokensExist = errors.New("auth.tokens already has entries")

// AddOperatorToken sets auth.mode to bearer and appends operatorToken, plus
// any extra tokens (in one atomic write — never a second, separate write
// that could land only one of them), into the Moltnet config file at path,
// reusing the same plaintext-preserving writeback machinery as WritePairing
// (untyped map edit, atomic write, mode 0600, symlink-refusing target). It
// refuses with ErrAuthTokensExist when auth.tokens already has any entries,
// so `moltnet init --bearer` rerun against an existing config only ever adds
// tokens to a genuinely token-less config, never silently appends a second
// admin-scoped credential next to one an operator already set up by hand.
//
// `moltnet init --bearer` calls this with both the operator token (all
// scopes, unchanged) and a second observe-only "console" token as extra, so
// a freshly bearer-enabled network always has a token `moltnet console` can
// hand the browser — closing the gap where init minted a token the console
// command was never allowed to use.
func AddOperatorToken(path string, operatorToken AuthTokenWriteback, extra ...AuthTokenWriteback) error {
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

	if auth, ok := doc["auth"].(map[string]any); ok && len(asMapSlice(auth["tokens"])) > 0 {
		return fmt.Errorf("%w in %s", ErrAuthTokensExist, path)
	}

	tokens := append([]AuthTokenWriteback{operatorToken}, extra...)
	return writeAuthTokensToDoc(path, format, doc, tokens)
}

// ErrConsoleTokenIDExists indicates AddConsoleToken's caller picked
// token.ID against a config it read at some earlier point (nextConsoleTokenID,
// cmd/moltnet/auth_token_writeback.go), but the config on disk — read again
// here, immediately before writing — already has a token using that same
// id by the time this function actually runs. AddConsoleToken refuses
// rather than silently overwrite it.
var ErrConsoleTokenIDExists = errors.New("console token id already exists")

// AddConsoleToken appends a single observe-only-scoped auth.tokens[] entry
// into the Moltnet config file at path, reusing the same writeback
// machinery as AddOperatorToken. Unlike AddOperatorToken, it never refuses
// because auth.tokens already has entries: it exists for `moltnet
// console`'s self-heal path, which by definition only ever runs against a
// config that already has at least one token (the operator's) but none
// scoped to exactly [observe] — refusing on "tokens already exist" would
// make it useless for its one caller.
//
// It does set auth.mode to bearer, via the same writeAuthTokensToDoc body
// AddOperatorToken shares — but for AddConsoleToken's one caller this is
// never an enabling side effect: self-heal only ever runs against a config
// that is already in bearer mode (that is its own precondition), so this
// write leaves auth.mode exactly as it found it in practice. It is
// documented here only because writeAuthTokensToDoc always does it,
// unconditionally, for both callers.
//
// The caller is responsible for first picking a non-colliding id (see the
// id-collision handling token.ID goes through before AddConsoleToken is
// called) — but that pick is made against a config read earlier, which can
// go stale by the time this function's own read happens (a concurrent
// `moltnet console` invocation, or a hand edit, landing in between). This
// function closes that TOCTOU itself: it checks the freshly re-decoded
// document for token.ID and refuses (ErrConsoleTokenIDExists) rather than
// trust the caller's earlier pick, so it never silently overwrites a token
// it was not asked to overwrite.
func AddConsoleToken(path string, token AuthTokenWriteback) error {
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

	if auth, ok := doc["auth"].(map[string]any); ok {
		for _, existing := range asMapSlice(auth["tokens"]) {
			if stringField(existing, "id") == token.ID {
				return fmt.Errorf("%w: %q in %s", ErrConsoleTokenIDExists, token.ID, path)
			}
		}
	}

	return writeAuthTokensToDoc(path, format, doc, []AuthTokenWriteback{token})
}

// writeAuthTokensToDoc is the shared body behind AddOperatorToken and
// AddConsoleToken: set auth.mode to bearer, upsert every token in tokens
// (by id — a rerun with the same id replaces rather than duplicates), and
// atomically write the encoded document back to path.
func writeAuthTokensToDoc(path, format string, doc map[string]any, tokens []AuthTokenWriteback) error {
	auth, _ := doc["auth"].(map[string]any)
	if auth == nil {
		auth = map[string]any{}
	}
	auth["mode"] = "bearer"
	doc["auth"] = auth
	for _, token := range tokens {
		upsertAuthToken(doc, token)
	}

	out, err := encodeWritebackDocument(format, doc)
	if err != nil {
		return fmt.Errorf("encode Moltnet config %q: %w", path, err)
	}

	return atomicWriteConfigFile(path, out)
}
