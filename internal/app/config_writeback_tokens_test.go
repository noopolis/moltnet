package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newConsoleToken() AuthTokenWriteback {
	return AuthTokenWriteback{
		ID:     "console",
		Value:  "console-token-value",
		Scopes: []string{"observe"},
	}
}

// TestAddOperatorTokenWithExtraWritesBothTokensAtomically covers item 1: a
// fresh config's `init --bearer` call mints the operator token and the
// console token in one AddOperatorToken call. Both must land in the same
// write, with distinct plaintext values and their own exact scopes.
func TestAddOperatorTokenWithExtraWritesBothTokensAtomically(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte("version: moltnet.v1\nnetwork:\n  id: alice-net\n  name: Alice's Moltnet\n"), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	if err := AddOperatorToken(path, newOperatorToken(), newConsoleToken()); err != nil {
		t.Fatalf("AddOperatorToken() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if config.Auth.Mode != "bearer" {
		t.Fatalf("auth.mode = %q, want bearer", config.Auth.Mode)
	}
	if len(config.Auth.Tokens) != 2 {
		t.Fatalf("expected exactly 2 auth.tokens[], got %d: %+v", len(config.Auth.Tokens), config.Auth.Tokens)
	}

	var operatorValue, consoleValue string
	for _, token := range config.Auth.Tokens {
		switch token.ID {
		case "operator":
			operatorValue = token.Value
			if len(token.Scopes) != 4 {
				t.Fatalf("expected the operator token to keep all 4 scopes, got %v", token.Scopes)
			}
		case "console":
			consoleValue = token.Value
			if len(token.Scopes) != 1 || string(token.Scopes[0]) != "observe" {
				t.Fatalf("expected the console token to carry exactly [observe], got %v", token.Scopes)
			}
		default:
			t.Fatalf("unexpected token id %q", token.ID)
		}
	}
	if operatorValue != "operator-token-value" || consoleValue != "console-token-value" {
		t.Fatalf("unexpected token values: operator=%q console=%q", operatorValue, consoleValue)
	}
	if operatorValue == consoleValue {
		t.Fatalf("expected distinct token values, both were %q", operatorValue)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if !strings.Contains(string(raw), "console-token-value") || !strings.Contains(string(raw), "operator-token-value") {
		t.Fatalf("expected both plaintext values on disk, got:\n%s", raw)
	}
}

// TestAddOperatorTokenWithExtraStillRefusesWhenTokensExist covers the flip
// side: the "config already has tokens" guard must still fire even when
// extra tokens are passed -- `init --bearer` must never silently append a
// second admin-scoped credential (or an unwanted console token) next to
// tokens an operator already set up by hand.
func TestAddOperatorTokenWithExtraStillRefusesWhenTokensExist(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}

	err = AddOperatorToken(path, newOperatorToken(), newConsoleToken())
	if err == nil {
		t.Fatal("expected AddOperatorToken() to refuse when auth.tokens already has entries")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after refused write: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("config was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestAddConsoleTokenAppendsAlongsideExistingTokens is the console
// self-heal case AddOperatorToken cannot serve: a config that already has
// auth.tokens (e.g. just an operator token) gets a console token appended,
// with no refusal.
func TestAddConsoleTokenAppendsAlongsideExistingTokens(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeWritebackFixture(t, path)

	if err := AddConsoleToken(path, AuthTokenWriteback{
		ID:     "console",
		Value:  "fresh-console-value",
		Scopes: []string{"observe"},
	}); err != nil {
		t.Fatalf("AddConsoleToken() error = %v", err)
	}

	config, err := LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Auth.Tokens) != 2 {
		t.Fatalf("expected the pre-existing token plus the new console token, got %d: %+v", len(config.Auth.Tokens), config.Auth.Tokens)
	}

	var foundExisting, foundConsole bool
	for _, token := range config.Auth.Tokens {
		switch token.ID {
		case "existing-token":
			foundExisting = true
			if token.Value != "existing-secret-value" {
				t.Fatalf("expected the pre-existing token's value untouched, got %q", token.Value)
			}
		case "console":
			foundConsole = true
			if token.Value != "fresh-console-value" {
				t.Fatalf("unexpected console token value %q", token.Value)
			}
			if len(token.Scopes) != 1 || string(token.Scopes[0]) != "observe" {
				t.Fatalf("expected the console token to carry exactly [observe], got %v", token.Scopes)
			}
		}
	}
	if !foundExisting || !foundConsole {
		t.Fatalf("expected both the pre-existing and new console tokens, got %+v", config.Auth.Tokens)
	}
}

// TestAddConsoleTokenRefusesWhenIDAlreadyPresent covers item 5's TOCTOU
// close: the caller (nextConsoleTokenID, cmd/moltnet/auth_token_writeback.go)
// picks a non-colliding id against a config it read at some earlier point,
// but the config on disk can have changed by the time AddConsoleToken's own
// read happens (a concurrent `moltnet console`, or a hand edit, landing in
// between). AddConsoleToken must refuse rather than silently overwrite
// whatever now uses that id -- and must leave the file untouched when it
// does.
func TestAddConsoleTokenRefusesWhenIDAlreadyPresent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(path, []byte(""+
		"version: moltnet.v1\n"+
		"network:\n"+
		"  id: toctou-net\n"+
		"  name: Toctou Moltnet\n"+
		"auth:\n"+
		"  mode: bearer\n"+
		"  tokens:\n"+
		"    - id: console\n"+
		"      value: raced-in-value\n"+
		"      scopes: [write]\n"), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}

	err = AddConsoleToken(path, AuthTokenWriteback{
		ID:     "console",
		Value:  "would-overwrite-value",
		Scopes: []string{"observe"},
	})
	if !errors.Is(err, ErrConsoleTokenIDExists) {
		t.Fatalf("AddConsoleToken() error = %v, want ErrConsoleTokenIDExists", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after refused write: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("config was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestAddOperatorTokenUpgradeClosesOpenAgentRegistration is P0-1's fix: an
// operator-only open config (defaultMoltnetConfig, cmd/moltnet/templates.go)
// carries an explicit auth.agent_registration: open, and config_load.go's
// mergeFileConfig only ever *re-derives* that field while auth.mode stays
// "open" -- it never clears an explicit value once mode moves off "open".
// Before this fix, AddOperatorToken's upgrade branch
// (canUpgradeOperatorOnlyOpenConfig) flipped auth.mode to "bearer" without
// touching that line, so it survived the upgrade verbatim: an operator who
// ran `init --bearer` believing they had locked the network down to their
// own token kept minting write-capable tokens for anyone who asked --
// exactly reversing bearerMoltnetConfig's own doc comment (templates.go)
// and the "close open registration" promise `init`'s own aftercare note
// makes elsewhere.
//
// This is checked functionally, against a real internal/app.App on both
// sides of the upgrade (assertAnonymousRegisterStatus), not just against
// the decoded Config struct: a struct can look right field-by-field while
// the route it is supposed to gate still behaves wrong.
// TestAddOperatorTokenUpgradeClosesOpenAgentRegistration (P0-1),
// TestAddOperatorTokenRefusesUpgradeWhenExistingTokenLacksAdminScope (P2-1),
// and TestAddOperatorTokenRefusesUpgradeWithMalformedNeighborToken (P2-1,
// sharpened) live in config_writeback_tokens_upgrade_test.go -- split out to
// keep this file under the repo's 400-line limit.

// TestAddConsoleTokenRejectsSymlinkedConfig mirrors
// TestAddOperatorTokenRejectsSymlinkedConfig for the second writeback entry
// point: neither ever writes through a symlinked config target.
func TestAddConsoleTokenRejectsSymlinkedConfig(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	writeWritebackFixture(t, target)

	link := filepath.Join(directory, "Moltnet")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if err := AddConsoleToken(link, newConsoleToken()); err == nil {
		t.Fatal("expected symlink rejection error")
	}
}
