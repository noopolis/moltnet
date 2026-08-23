package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

// This file covers PLAN.md phase 7.0 ("the operator credential") and phase
// 7A.2/7A.federation (the starter room's federation:none stance) end to
// end, against real `runInit` output and, where it matters, a real
// internal/app.App on a real loopback listener (startRealMoltnetServer,
// console_e2e_test.go) rather than a hand-built fixture — the point of
// these five is to prove the actual `moltnet init` path, not a
// representative approximation of it.

// TestRunInitStarterRoomSurvivesPairInviteReload is the landmine test
// PLAN.md 7A.federation calls for: without the starter "general" room
// declaring an explicit federation:none stance, this fails the moment
// pairings[] goes non-empty, because validateRoomFederationStances
// (internal/app/config_file.go) errors on ANY room with a nil federation
// stance once pairings[] is non-empty -- not just the room named by
// --room -- and writePairingWithRollback (pair.go) rolls the whole pairing
// back when that reload fails. `pair invite --room chat` never touches
// "general" at all, so this only passes if "general" already carries its
// own explicit stance from `init` itself.
func TestRunInitStarterRoomSurvivesPairInviteReload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")

	before, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() before pairing error = %v", err)
	}
	general := findRoom(t, before, "general")
	if general.Federation == nil {
		t.Fatal("expected the starter room to already declare an explicit federation stance right after init, before any pairing exists")
	}

	if err := run(context.Background(), []string{
		"pair", "invite",
		"--relay-url", "wss://moltnet-relay.acme.workers.dev",
		"--relay-token-env", "TEST_RELAY_TOKEN",
		"--id", "friend-net",
		"--room", "chat",
		"--config", configPath,
	}, "test"); err != nil {
		t.Fatalf("pair invite against a freshly-init'ed config error = %v -- this is exactly the landmine PLAN.md 7A.federation exists to prevent", err)
	}

	after, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after pairing error = %v", err)
	}
	if len(after.Pairings) != 1 {
		t.Fatalf("expected exactly one pairing to have been written, got %d", len(after.Pairings))
	}
	stillThere := findRoom(t, after, "general")
	if stillThere.Federation == nil {
		t.Fatal("expected the starter room's federation stance to survive the pairing write")
	}
}

// TestRunInitOpenModeStillAllowsAnonymousRegistration is the mode-
// preservation test: plain `init`'s new operator token must not flip
// auth.mode away from "open" (defaultMoltnetConfig, templates.go) --
// verified both by reading the written config back and, functionally, by
// proving anonymous POST /v1/agents/register still succeeds against a real
// running server built from it.
func TestRunInitOpenModeStillAllowsAnonymousRegistration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")

	config, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if config.Auth.Mode != "open" {
		t.Fatalf("auth.mode = %q, want open -- the operator token must never flip this", config.Auth.Mode)
	}
	if len(config.Auth.Tokens) != 1 || config.Auth.Tokens[0].ID != "operator" {
		t.Fatalf("expected exactly one operator token, got %+v", config.Auth.Tokens)
	}

	rewriteConfigListenAddr(t, configPath, freeLoopbackAddr(t))
	server := startRealMoltnetServer(t, configPath)

	response, err := http.Post(server.URL+"/v1/agents/register", "application/json", strings.NewReader(`{"requested_agent_id":"scout"}`))
	if err != nil {
		t.Fatalf("POST /v1/agents/register error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("anonymous register against a plain-init'ed network = %d, want %d: %s", response.StatusCode, http.StatusCreated, body)
	}
}

// TestRunInitOperatorTokenGrantsAdminAccess is the admin-works test:
// adminAuthFromServerConfig must find the operator token init just minted,
// report it as bearer-mode admin access, and that token must actually
// authenticate an existing admin route against a real running server --
// `admin agent remove`, not `room create` (PLAN.md 7A.1, not shipped by
// this unit).
func TestRunInitOperatorTokenGrantsAdminAccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	rewriteConfigListenAddr(t, configPath, freeLoopbackAddr(t))
	server := startRealMoltnetServer(t, configPath)

	config, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	auth := adminAuthFromServerConfig(config)
	if auth.Mode != bridgeconfig.AuthModeBearer {
		t.Fatalf("adminAuthFromServerConfig mode = %q, want bearer", auth.Mode)
	}
	if strings.TrimSpace(auth.Token) == "" {
		t.Fatal("adminAuthFromServerConfig returned an empty admin token")
	}

	// Register an agent (open self-registration, no operator credential
	// involved) so the admin route below has something real to act on.
	registerResponse, err := http.Post(server.URL+"/v1/agents/register", "application/json", strings.NewReader(`{"requested_agent_id":"scout"}`))
	if err != nil {
		t.Fatalf("POST /v1/agents/register error = %v", err)
	}
	registerResponse.Body.Close()
	if registerResponse.StatusCode != http.StatusCreated {
		t.Fatalf("register scout = %d, want %d", registerResponse.StatusCode, http.StatusCreated)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"admin", "agent", "remove", "--agent", "scout", "--network", "acme",
		}, "test"); err != nil {
			t.Fatalf("admin agent remove with the plain-init operator token error = %v", err)
		}
	})
	if !strings.Contains(output, `"removed": true`) {
		t.Fatalf("unexpected admin agent remove output %q", output)
	}
}

// TestRunInitRepairsExistingTokenlessOpenConfig is the repair test: plain
// `init` re-run against a pre-7.0, tokenless open-mode config (the shape
// every network created before this phase shipped has) inserts the
// operator token in place and leaves auth.mode exactly at "open".
func TestRunInitRepairsExistingTokenlessOpenConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".moltnet", "legacy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	configPath := filepath.Join(dir, "Moltnet")
	legacyContents := "" +
		"version: moltnet.v1\n\n" +
		"network:\n  id: legacy\n  name: Legacy Moltnet\n\n" +
		"server:\n  listen_addr: \"127.0.0.1:8787\"\n\n" +
		"auth:\n  mode: open\n  agent_registration: open\n\n" +
		"storage:\n  kind: sqlite\n  sqlite:\n    path: .moltnet/moltnet.db\n\n" +
		"rooms: []\npairings: []\n"
	if err := os.WriteFile(configPath, []byte(legacyContents), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "legacy", "--verbose"}); err != nil {
			t.Fatalf("runInit() repair error = %v", err)
		}
	})
	if !strings.Contains(output, "added an operator token") {
		t.Fatalf("expected the repair checkmark in verbose output, got %q", output)
	}
	if strings.Contains(output, "already has auth.tokens") {
		t.Fatalf("did not expect the skip note on a genuinely tokenless config, got %q", output)
	}

	config, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if config.Auth.Mode != "open" {
		t.Fatalf("auth.mode = %q, want open (the repair must never flip it)", config.Auth.Mode)
	}
	if len(config.Auth.Tokens) != 1 {
		t.Fatalf("expected exactly one repaired operator token, got %d: %+v", len(config.Auth.Tokens), config.Auth.Tokens)
	}
	token := config.Auth.Tokens[0]
	if token.ID != "operator" {
		t.Fatalf("token id = %q, want operator", token.ID)
	}
	if len(token.Scopes) != 3 {
		t.Fatalf("expected exactly 3 scopes (never \"pair\"), got %v", token.Scopes)
	}
	for _, scope := range token.Scopes {
		if string(scope) == "pair" {
			t.Fatal("a repaired operator token must never carry the pair scope")
		}
	}
}

// TestRunInitRepairSkipsWhenTokensAlreadyExist covers PLAN.md 7.0's
// documented decision for the case it recommends against guessing at: an
// existing tokenless-repair candidate that turns out to already carry
// auth.tokens (e.g. an operator's own hand-added credential) is left
// completely untouched, and plain `init` prints a clear note explaining
// why rather than silently doing nothing.
func TestRunInitRepairSkipsWhenTokensAlreadyExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".moltnet", "legacy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	configPath := filepath.Join(dir, "Moltnet")
	legacyContents := "" +
		"version: moltnet.v1\n\n" +
		"network:\n  id: legacy\n  name: Legacy Moltnet\n\n" +
		"server:\n  listen_addr: \"127.0.0.1:8787\"\n\n" +
		"auth:\n  mode: open\n  agent_registration: open\n  tokens:\n" +
		"    - id: hand-added\n      value: hand-secret-value\n      scopes: [observe]\n\n" +
		"storage:\n  kind: sqlite\n  sqlite:\n    path: .moltnet/moltnet.db\n\n" +
		"rooms: []\npairings: []\n"
	if err := os.WriteFile(configPath, []byte(legacyContents), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read fixture before init: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "legacy"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	if !strings.Contains(output, "note:") || !strings.Contains(output, "already has auth.tokens") {
		t.Fatalf("expected a clear note when the repair is skipped, got %q", output)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read fixture after init: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected the config to be left byte-for-byte untouched when the repair is skipped:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestRunInitRepairSkipNoteFiresForOperatorNamedTokenLackingAdminScope is
// round 2's P3 fix to existingConfigHasAdminToken (auth_token_writeback.go):
// it used to treat any token literally named "operator" as admin-capable
// regardless of its actual scopes, so a hand-written
// `id: operator, scopes: [observe]` config was told it needed no repair --
// disagreeing outright with `init --bearer`'s own
// canUpgradeOperatorOnlyOpenConfig (config_writeback_tokens.go), which
// refuses that exact shape because it lacks admin (and write). Plain init
// against this shape must print the skip note, not stay silent about a
// network that in fact has no way to be administered.
func TestRunInitRepairSkipNoteFiresForOperatorNamedTokenLackingAdminScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".moltnet", "legacy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	configPath := filepath.Join(dir, "Moltnet")
	legacyContents := "" +
		"version: moltnet.v1\n\n" +
		"network:\n  id: legacy\n  name: Legacy Moltnet\n\n" +
		"server:\n  listen_addr: \"127.0.0.1:8787\"\n\n" +
		"auth:\n  mode: open\n  agent_registration: open\n  tokens:\n" +
		"    - id: operator\n      value: observe-only-value\n      scopes: [observe]\n\n" +
		"storage:\n  kind: sqlite\n  sqlite:\n    path: .moltnet/moltnet.db\n\n" +
		"rooms: []\npairings: []\n"
	if err := os.WriteFile(configPath, []byte(legacyContents), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "legacy"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	if !strings.Contains(output, "note:") || !strings.Contains(output, "already has auth.tokens") {
		t.Fatalf("expected the repair-skip note for a scope-lacking \"operator\"-named token, got %q", output)
	}
}

// TestRunInitBearerSymlinkNoteDoesNotDuplicate is round 2's P3 fix: the
// --bearer symlink note used to nest two messages that each independently
// named the config path and gave "edit auth: there ... to add an operator
// token" advice -- init_server_config.go's own inner error carried both,
// and init_summary.go's wrapper around every bearerAddErr adds them again
// unconditionally. The printed line said the path twice and the same advice
// twice. The inner error now states only why the write was refused; the
// wrapper alone owns the path and the actionable next step.
func TestRunInitBearerSymlinkNoteDoesNotDuplicate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	networkDir := filepath.Join(home, ".moltnet", "acme")
	if err := os.MkdirAll(networkDir, 0o700); err != nil {
		t.Fatalf("mkdir network dir: %v", err)
	}
	realConfigPath := filepath.Join(t.TempDir(), "real-Moltnet")
	if err := os.WriteFile(realConfigPath, []byte("version: moltnet.v1\nnetwork:\n  id: acme\n  name: Acme Moltnet\n"), 0o600); err != nil {
		t.Fatalf("write real config: %v", err)
	}
	symlinkPath := filepath.Join(networkDir, "Moltnet")
	if err := os.Symlink(realConfigPath, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	abbreviatedPath := abbreviateHome(symlinkPath)
	if count := strings.Count(output, abbreviatedPath); count != 1 {
		t.Fatalf("expected the config path to appear exactly once in the note, got %d times in %q", count, output)
	}
	if count := strings.Count(output, "edit auth:"); count != 1 {
		t.Fatalf("expected exactly one \"edit auth:\" mention, got %d in %q", count, output)
	}
}

// TestRunInitRerunOnHealthyNetworkStaysQuiet (P1-2) and
// TestRunInitStarterRoomIsAnonymouslyReadable (P2-2) live in
// init_anonymous_access_test.go -- split out to keep this file under the
// repo's 400-line limit.
