package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrAuthTokensExist indicates auth.tokens[] in the target Moltnet config
// already has at least one entry that AddOperatorToken cannot safely
// upgrade in place (canUpgradeOperatorOnlyOpenConfig), so it refuses rather
// than risk creating an unwanted second admin-scoped credential or silently
// reshaping tokens an operator configured by hand.
var ErrAuthTokensExist = errors.New("auth.tokens already has entries")

// AddOperatorToken sets auth.mode to bearer and appends operatorToken, plus
// any extra tokens (in one atomic write — never a second, separate write
// that could land only one of them), into the Moltnet config file at path,
// reusing the same plaintext-preserving writeback machinery as WritePairing
// (untyped map edit, atomic write, mode 0600, symlink-refusing target).
//
// It refuses with ErrAuthTokensExist when auth.tokens already has entries
// it cannot safely upgrade — with one exception (PLAN.md phase 7.0's
// "unblock the --bearer upgrade" fix, added once plain `init` started
// minting its own operator token, below): when the config's *only* existing
// token shares operatorToken.ID, canUpgradeOperatorOnlyOpenConfig treats it
// as exactly the operator-only open config plain `init` writes, and this
// function upgrades it in place — flipping auth.mode to bearer (via
// writeAuthTokensToDoc, shared with AddConsoleToken) and writing only the
// extra tokens, while leaving the existing operator entry completely
// untouched. That is deliberate, not an oversight: re-writing it here would
// silently rotate the operator's secret and could widen its scopes to
// whatever the caller passed (bearerMoltnetConfig-shaped callers pass
// operatorToken with the "pair" scope plain `init`'s operator token must
// never carry — see defaultMoltnetConfig's doc comment, templates.go).
// Every other existing-tokens shape (more than one token, or a single token
// under a different id) still refuses outright, so `init --bearer` never
// reshapes a config an operator set up by hand.
//
// `moltnet init --bearer` calls this with both the operator token
// ([observe, write, admin], unchanged) and a second observe-only "console"
// token as extra, so
// a freshly bearer-enabled network always has a token `moltnet console` can
// hand the browser — closing the gap where init minted a token the console
// command was never allowed to use. On the upgrade path above, only that
// console token actually lands; the operator token argument is discarded
// once its id is confirmed to already exist.
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

	auth, _ := doc["auth"].(map[string]any)
	hasExistingTokens := authHasAnyTokens(auth)

	tokens := append([]AuthTokenWriteback{operatorToken}, extra...)
	if hasExistingTokens {
		if !canUpgradeOperatorOnlyOpenConfig(doc, operatorToken.ID) {
			return fmt.Errorf("%w in %s", ErrAuthTokensExist, path)
		}
		tokens = extra
	}

	return writeAuthTokensToDoc(path, format, doc, tokens)
}

// canUpgradeOperatorOnlyOpenConfig reports whether doc is exactly the
// operator-only open config phase 7.0's plain `init` writes: auth.mode
// "open", with a single auth.tokens[] entry that is itself the plain-init
// operator token shape -- id operatorTokenID, a non-empty value, both
// "admin" and "write" among its scopes, and neither a network nor an agent
// restriction (defaultMoltnetConfig, templates.go, sets none of those).
// AddOperatorToken treats only this one shape as safe to upgrade in place;
// every other combination (more than one token, a differently-id'd or
// differently-scoped single token, a restricted token, or a mode other than
// "open") keeps refusing with ErrAuthTokensExist, so `init --bearer` can
// never silently reshape a config an operator configured by hand.
//
// P2-1: scopes were not always inspected here -- an id/mode/count match
// alone let a hand-written config through even when its lone "operator"
// token was scoped down to e.g. [observe]. That token has no admin access
// to begin with, so "upgrading" it (flip to bearer, leave the token as-is)
// silently produced a bearer network with zero admin-capable tokens while
// still reporting success. Requiring admin and write here closes that: a
// token that cannot already administer the network is never mistaken for
// the plain-init shape, and the caller's outright refusal (with its
// actionable note) fires instead.
//
// The raw auth.tokens[] length is read directly from the decoded document
// (auth["tokens"].([]any)) rather than through asMapSlice: asMapSlice
// silently drops list entries that fail its map[string]any assertion
// (config_writeback.go), so a config whose auth.tokens[] has, say, one
// malformed entry alongside a real one would present as length-1 through
// that helper even though the file genuinely has two entries. Checking the
// raw slice length first means a malformed neighbor entry always refuses
// this upgrade rather than being silently filtered out from under it.
func canUpgradeOperatorOnlyOpenConfig(doc map[string]any, operatorTokenID string) bool {
	auth, ok := doc["auth"].(map[string]any)
	if !ok {
		return false
	}

	// P2-1 (round 2, security): public_read is config_load.go's other
	// open-mode derivation (mergeFileConfig forces it true whenever
	// auth.mode is "open", exactly like agent_registration above it) --
	// defaultMoltnetConfig never writes it, so the shipped plain-init shape
	// never carries it, but a hand-written config that states its derived
	// posture explicitly (auth.public_read: true) was carried across this
	// upgrade verbatim: AddOperatorToken deletes agent_registration but
	// never public_read, so a network "locked down" via `init --bearer`
	// could keep serving anonymous GET /v1/rooms and .../messages on both
	// REST and SSE. Rather than special-case public_read (and every future
	// field config_load.go learns to derive from mode: open the same way),
	// refuse the upgrade outright whenever auth carries any key this exact
	// plain-init shape does not: "never reshape a hand-configured config"
	// is the invariant this guard exists to hold, not "delete one more
	// field whenever a review finds it."
	for key := range auth {
		switch key {
		case "mode", "agent_registration", "tokens":
		default:
			return false
		}
	}

	if strings.TrimSpace(stringField(auth, "mode")) != "open" {
		return false
	}

	rawTokens, _ := auth["tokens"].([]any)
	if len(rawTokens) != 1 {
		return false
	}
	token, ok := rawTokens[0].(map[string]any)
	if !ok {
		return false
	}

	if stringField(token, "id") != operatorTokenID {
		return false
	}
	if strings.TrimSpace(stringField(token, "value")) == "" {
		return false
	}
	// A network- or agent-restricted token is never what defaultMoltnetConfig
	// writes -- it is either a hand-narrowed credential or a token that was
	// never meant to administer this whole network, and the plain-init
	// shape's own scopes (below) already establish admin access some other
	// way if this upgrade is going to trust it.
	if strings.TrimSpace(stringField(token, "network")) != "" {
		return false
	}
	if agents, ok := token["agents"].([]any); ok && len(agents) > 0 {
		return false
	}

	scopes, _ := token["scopes"].([]any)
	var hasAdmin, hasWrite bool
	for _, scope := range scopes {
		switch s, _ := scope.(string); s {
		case "admin":
			hasAdmin = true
		case "write":
			hasWrite = true
		}
	}
	return hasAdmin && hasWrite
}

// ErrAuthModeNotOpen indicates AddOperatorTokenPreservingOpenMode was asked
// to repair a Moltnet config whose auth.mode is not "open" — outside the one
// shape PLAN.md phase 7.0's repair path exists for, so it refuses rather
// than guess what a non-open network wants.
var ErrAuthModeNotOpen = errors.New("auth.mode is not open")

// AddOperatorTokenPreservingOpenMode inserts operatorToken into an existing
// Moltnet config's auth.tokens[] without ever touching auth.mode — unlike
// writeAuthTokensToDoc (shared by AddOperatorToken and AddConsoleToken),
// which always flips it to "bearer". It backs plain `init`'s PLAN.md phase
// 7.0 repair path: a config written before phase 7.0 shipped has
// auth.mode: open and no operator token at all, so its owner has no way to
// administer their own network; re-running plain `init` against it now
// fixes that in place, atomically, while leaving the network's local-first
// "open" posture exactly as the owner left it.
//
// It only ever writes when the config is genuinely that shape: auth.mode
// already "open" and auth.tokens[] currently empty. Anything else refuses
// rather than guess:
//   - auth.mode not "open" returns ErrAuthModeNotOpen — outside this
//     repair's stated scope (a hand-edited bearer/none config), and not
//     plain `init`'s concern either way, since it has never rewritten an
//     existing config's auth.mode.
//   - any existing auth.tokens[] entries return ErrAuthTokensExist —
//     PLAN.md phase 7.0 leaves such a config untouched rather than
//     guessing whether adding a second admin-scoped credential next to
//     whatever an operator already configured by hand is safe; the caller
//     (runInit) prints a note explaining that rather than silently doing
//     nothing.
func AddOperatorTokenPreservingOpenMode(path string, operatorToken AuthTokenWriteback) error {
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

	auth, _ := doc["auth"].(map[string]any)
	if auth == nil || strings.TrimSpace(stringField(auth, "mode")) != "open" {
		return fmt.Errorf("%w in %s", ErrAuthModeNotOpen, path)
	}
	if authHasAnyTokens(auth) {
		return fmt.Errorf("%w in %s", ErrAuthTokensExist, path)
	}

	upsertAuthToken(doc, operatorToken)
	// auth.mode is left exactly as read ("open") — deliberately not routed
	// through writeAuthTokensToDoc, which both AddOperatorToken and
	// AddConsoleToken share and which always sets it to "bearer". Flipping
	// it here would silently reverse phase 6a's local-first default for a
	// network whose owner only ever ran plain `init` again.

	out, err := encodeWritebackDocument(format, doc)
	if err != nil {
		return fmt.Errorf("encode Moltnet config %q: %w", path, err)
	}

	return atomicWriteConfigFile(path, out)
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
//
// P0-2 (confirmed live, round 2): the delete(auth, "agent_registration")
// below used to live only inside AddOperatorToken's hasExistingTokens
// branch, guarding just the operator-only-open upgrade path. A pre-7.0
// legacy config with *no* tokens at all skips that branch entirely
// (hasExistingTokens is false), so `init --bearer` against it flipped
// auth.mode to bearer via this exact function while an explicit
// auth.agent_registration: open survived verbatim -- anonymous
// POST /v1/agents/register kept minting write-capable tokens on a network
// that just turned bearer auth on, directly contradicting
// bearerMoltnetConfig's own doc comment (templates.go). This is the same
// defect an earlier review round already fixed on the operator-only
// branch; the sibling branch (and every other path that can reach this
// function, including AddConsoleToken's self-heal) was never checked.
// Doing the delete here, unconditionally, on every write that sets
// auth.mode to bearer -- rather than duplicating it per call site -- means
// no future caller of this shared body can reintroduce the same gap by
// skipping a branch-local guard. config_load.go's mergeFileConfig only
// ever *re-derives* agent_registration from an "open" mode, it never
// clears an explicit value once mode moves off "open", so this key never
// self-heals on its own; deleting it (rather than writing "disabled") lets
// defaultConfig's own AgentRegistrationDisabled zero value govern it,
// matching what a fresh `init --bearer` config (bearerMoltnetConfig)
// leaves unset for the exact same reason.
func writeAuthTokensToDoc(path, format string, doc map[string]any, tokens []AuthTokenWriteback) error {
	auth, _ := doc["auth"].(map[string]any)
	if auth == nil {
		auth = map[string]any{}
	}
	auth["mode"] = "bearer"
	delete(auth, "agent_registration")
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

// authHasAnyTokens reports whether auth's "tokens" field is present in any
// non-empty form: a well-formed list with at least one entry, or any other
// non-nil value at all (a map, a bare string, a list made entirely of
// malformed entries, ...). AddOperatorToken and
// AddOperatorTokenPreservingOpenMode both gate their refusal guard on this
// rather than on len(asMapSlice(auth["tokens"])) > 0 (round 2's P2-2):
// asMapSlice silently drops any list entry that fails its map[string]any
// assertion, so auth.tokens[] made entirely of malformed entries -- a map
// in place of a list, a bare string, or a list holding one non-map entry --
// presented as length-0 through that helper even though the file genuinely
// had a "tokens" field. That let both callers skip their refusal guard
// entirely and silently delete-and-replace the malformed value on a
// successful write the rollback wrapper's post-write reload could never
// catch. Routing "present but malformed" back into the same guard that
// handles "present and well-formed" -- canUpgradeOperatorOnlyOpenConfig for
// AddOperatorToken, the caller's own check for
// AddOperatorTokenPreservingOpenMode -- means a malformed tokens field
// always refuses rather than being silently overwritten.
func authHasAnyTokens(auth map[string]any) bool {
	if auth == nil {
		return false
	}
	switch raw := auth["tokens"].(type) {
	case nil:
		return false
	case []any:
		return len(raw) > 0
	default:
		return true
	}
}
