package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
)

// This file covers AddOperatorToken's canUpgradeOperatorOnlyOpenConfig
// upgrade path specifically (P0-1 and P2-1) -- split out of
// config_writeback_tokens_test.go to keep that file under the repo's
// 400-line limit.

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
func TestAddOperatorTokenUpgradeClosesOpenAgentRegistration(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	fixture := "" +
		"version: moltnet.v1\n" +
		"network:\n  id: acme\n  name: Acme Moltnet\n" +
		"auth:\n" +
		"  mode: open\n" +
		"  agent_registration: open\n" +
		"  tokens:\n" +
		"    - id: operator\n" +
		"      value: operator-token-value\n" +
		"      scopes: [observe, write, admin]\n" +
		"storage:\n  kind: memory\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	beforeConfig, err := LoadConfigForPath(path, "test")
	if err != nil {
		t.Fatalf("LoadConfigForPath() before upgrade error = %v", err)
	}
	if beforeConfig.Auth.AgentRegistration != authn.AgentRegistrationOpen {
		t.Fatalf("sanity check failed: agent_registration = %q before the upgrade, want open", beforeConfig.Auth.AgentRegistration)
	}
	assertAnonymousRegisterStatus(t, beforeConfig, http.StatusCreated)

	if err := AddOperatorToken(path, AuthTokenWriteback{
		ID:     "operator",
		Value:  "operator-token-value",
		Scopes: []string{"observe", "write", "admin"},
	}, newConsoleToken()); err != nil {
		t.Fatalf("AddOperatorToken() error = %v", err)
	}

	afterConfig, err := LoadConfigForPath(path, "test")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after upgrade error = %v", err)
	}
	if afterConfig.Auth.Mode != "bearer" {
		t.Fatalf("auth.mode = %q, want bearer", afterConfig.Auth.Mode)
	}
	if afterConfig.Auth.AgentRegistration == authn.AgentRegistrationOpen {
		t.Fatalf("expected the --bearer upgrade to close agent_registration, got %q", afterConfig.Auth.AgentRegistration)
	}
	assertAnonymousRegisterStatus(t, afterConfig, http.StatusUnauthorized)
}

// assertAnonymousRegisterStatus starts a real internal/app.App from config
// and asserts the HTTP status an anonymous (no Authorization header)
// POST /v1/agents/register receives.
func assertAnonymousRegisterStatus(t *testing.T, config Config, want int) {
	t.Helper()

	instance, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer instance.close()

	server := httptest.NewServer(instance.server.Handler)
	defer server.Close()

	response, err := http.Post(server.URL+"/v1/agents/register", "application/json", strings.NewReader(`{"requested_agent_id":"spoof"}`))
	if err != nil {
		t.Fatalf("POST /v1/agents/register error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("anonymous register status = %d, want %d", response.StatusCode, want)
	}
}

// TestAddOperatorTokenRefusesUpgradeWhenExistingTokenLacksAdminScope is
// P2-1's fix: canUpgradeOperatorOnlyOpenConfig used to match on
// id/mode/count alone, never inspecting scopes. Verified live against a
// hand-written open config whose lone "operator" token was scoped only
// [observe]: the old code treated it as the plain-init shape, flipped
// auth.mode to bearer, and left the network with zero admin-capable tokens
// while still reporting success. This must instead keep refusing with
// ErrAuthTokensExist, leaving the file untouched, so the caller's own
// actionable note fires instead of a silent lockout.
func TestAddOperatorTokenRefusesUpgradeWhenExistingTokenLacksAdminScope(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	fixture := "" +
		"version: moltnet.v1\n" +
		"network:\n  id: acme\n  name: Acme Moltnet\n" +
		"auth:\n" +
		"  mode: open\n" +
		"  tokens:\n" +
		"    - id: operator\n" +
		"      value: observe-only-value\n" +
		"      scopes: [observe]\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	err = AddOperatorToken(path, newOperatorToken(), newConsoleToken())
	if !errors.Is(err, ErrAuthTokensExist) {
		t.Fatalf("AddOperatorToken() error = %v, want ErrAuthTokensExist", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after refused write: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("config was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestAddOperatorTokenRefusesUpgradeWithMalformedNeighborToken pins a second,
// independent security review's sharpening of P2-1: asMapSlice silently
// drops list entries that fail its map[string]any assertion
// (config_writeback.go), so a raw auth.tokens[] of length 2 -- one
// well-formed "operator" entry plus one malformed (non-map) neighbor -- used
// to present as length-1 through that helper and pass a naive
// `len(asMapSlice(...)) != 1` check even though the file genuinely has two
// entries. canUpgradeOperatorOnlyOpenConfig reads the raw slice length
// directly instead, so a malformed neighbor always refuses this upgrade
// rather than being silently filtered out from under it.
func TestAddOperatorTokenRefusesUpgradeWithMalformedNeighborToken(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	fixture := "" +
		"version: moltnet.v1\n" +
		"network:\n  id: acme\n  name: Acme Moltnet\n" +
		"auth:\n" +
		"  mode: open\n" +
		"  tokens:\n" +
		"    - id: operator\n" +
		"      value: operator-token-value\n" +
		"      scopes: [observe, write, admin]\n" +
		"    - not-a-map-entry\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	err = AddOperatorToken(path, AuthTokenWriteback{
		ID:     "operator",
		Value:  "operator-token-value",
		Scopes: []string{"observe", "write", "admin"},
	}, newConsoleToken())
	if !errors.Is(err, ErrAuthTokensExist) {
		t.Fatalf("AddOperatorToken() error = %v, want ErrAuthTokensExist", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after refused write: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("config was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestAddOperatorTokenRefusesUpgradeWhenPublicReadIsSet is round 2's P2-1:
// public_read is config_load.go's other open-mode derivation
// (mergeFileConfig forces it true whenever auth.mode is "open", exactly
// like agent_registration) -- defaultMoltnetConfig never writes it, so the
// shipped plain-init shape never carries it, but a hand-written config that
// states its derived posture explicitly (auth.public_read: true) used to
// survive AddOperatorToken's upgrade verbatim: the upgrade deletes
// agent_registration but never touched public_read, so a network "locked
// down" via `init --bearer` kept serving anonymous
// GET /v1/rooms and .../messages afterward. canUpgradeOperatorOnlyOpenConfig
// now refuses the upgrade outright whenever auth carries any key outside
// {mode, agent_registration, tokens}, so public_read (or any future
// open-mode derivation config_load.go learns) is never silently carried
// across.
func TestAddOperatorTokenRefusesUpgradeWhenPublicReadIsSet(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	fixture := "" +
		"version: moltnet.v1\n" +
		"network:\n  id: acme\n  name: Acme Moltnet\n" +
		"auth:\n" +
		"  mode: open\n" +
		"  public_read: true\n" +
		"  tokens:\n" +
		"    - id: operator\n" +
		"      value: operator-token-value\n" +
		"      scopes: [observe, write, admin]\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	err = AddOperatorToken(path, AuthTokenWriteback{
		ID:     "operator",
		Value:  "operator-token-value",
		Scopes: []string{"observe", "write", "admin"},
	}, newConsoleToken())
	if !errors.Is(err, ErrAuthTokensExist) {
		t.Fatalf("AddOperatorToken() error = %v, want ErrAuthTokensExist", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after refused write: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("config was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestAddOperatorTokenRefusesUpgradeWithMalformedTokensShapes is round 2's
// P2-2: AddOperatorToken used to gate its "tokens already exist" guard on
// len(asMapSlice(auth["tokens"])) > 0, and asMapSlice silently drops any
// list entry that fails its map[string]any assertion -- so auth.tokens[]
// written as a map, a bare string, or a list holding one non-map entry all
// presented as length-0 through that helper even though the file genuinely
// had a "tokens" field. That skipped canUpgradeOperatorOnlyOpenConfig
// entirely and fell through to a plain write, silently deleting and
// replacing the malformed value with a fresh operator+console pair, exit 0,
// with no error the rollback wrapper's post-write reload could ever catch
// (the replacement is well-formed). authHasAnyTokens now treats every one
// of these shapes as "tokens exist," routing them back through the real
// refusal.
func TestAddOperatorTokenRefusesUpgradeWithMalformedTokensShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		tokens string
	}{
		{name: "map", tokens: "  tokens:\n    foo: bar\n"},
		{name: "string", tokens: "  tokens: not-a-list\n"},
		{name: "list of one non-map entry", tokens: "  tokens:\n    - not-a-map-entry\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			path := filepath.Join(directory, "Moltnet")
			fixture := "" +
				"version: moltnet.v1\n" +
				"network:\n  id: acme\n  name: Acme Moltnet\n" +
				"auth:\n" +
				"  mode: open\n" +
				tc.tokens
			if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
				t.Fatalf("write fixture config: %v", err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			err = AddOperatorToken(path, newOperatorToken(), newConsoleToken())
			if !errors.Is(err, ErrAuthTokensExist) {
				t.Fatalf("AddOperatorToken() error = %v, want ErrAuthTokensExist", err)
			}

			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read config after refused write: %v", err)
			}
			if string(before) != string(after) {
				t.Fatalf("config was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// TestAddOperatorTokenPreservingOpenModeRefusesMalformedTokensShape covers
// the second call site sharing round 2's P2-2 bug: plain `init`'s repair
// path (AddOperatorTokenPreservingOpenMode) must also refuse rather than
// silently overwrite a malformed auth.tokens[] value instead of reporting
// it as already having entries.
func TestAddOperatorTokenPreservingOpenModeRefusesMalformedTokensShape(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	fixture := "" +
		"version: moltnet.v1\n" +
		"network:\n  id: acme\n  name: Acme Moltnet\n" +
		"auth:\n" +
		"  mode: open\n" +
		"  tokens: not-a-list\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	err = AddOperatorTokenPreservingOpenMode(path, newOperatorToken())
	if !errors.Is(err, ErrAuthTokensExist) {
		t.Fatalf("AddOperatorTokenPreservingOpenMode() error = %v, want ErrAuthTokensExist", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after refused write: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("config was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
