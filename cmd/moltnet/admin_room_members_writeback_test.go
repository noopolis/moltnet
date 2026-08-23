package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// startRestartableMoltnetServer is startRealMoltnetServer's (console_e2e_test.go)
// counterpart for tests that need to actually simulate a restart: it hands
// back the *app.App instance itself, not just the httptest.Server, so the
// caller can Close() it explicitly before building a second instance
// against the same (sqlite-backed) config -- proving a config writeback
// actually survives a fresh app.New(), not merely that the same running
// process kept its in-memory state.
func startRestartableMoltnetServer(t *testing.T, path string) (*httptest.Server, *app.App) {
	t.Helper()
	cfg, err := app.LoadConfigForPath(path, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q): %v", path, err)
	}
	instance, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New(): %v", err)
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		t.Fatalf("listen on %q: %v", cfg.ListenAddr, err)
	}
	server := httptest.NewUnstartedServer(instance.Handler())
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	return server, instance
}

// admin_room_members_writeback_test.go covers 7B.5's required test: members
// added via `admin room members add` must survive a restart, closing the bug
// PLAN.md 7A.1 documented and reproduced live ("chat members = [alice]
// before restart, nil after"). Storage is sqlite, not memory, so the second
// app.New() below genuinely reloads state from disk rather than reusing the
// first instance's in-memory store.
func TestAdminRoomMembersAddSurvivesRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, `version: moltnet.v1

network:
  id: chat-net
  name: "Chat Net"

server:
  listen_addr: "`+addr+`"

storage:
  kind: sqlite
  sqlite:
    path: .moltnet/moltnet.db

auth:
  mode: bearer
  tokens:
    - id: operator
      value: operator-secret
      scopes: [observe, write, admin]

rooms:
  - id: chat
    federation: none

pairings: []
`)

	server, instance := startRestartableMoltnetServer(t, configPath)

	// Deliberately no --base-url/--token: this is the exact zero-flag
	// aftercare shape pair_aftercare.go's own printed hint uses when run on
	// the same machine as the server ("admin derives both from that
	// network's own server config automatically"), and it is also the one
	// shape adminRoomMembersConfigWriteback (admin_room_members_writeback.go)
	// treats as unambiguously local.
	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"admin", "room", "members", "add",
			"--network", "chat-net",
			"--room", "chat",
			"--member", "alice",
		}, "test"); err != nil {
			t.Fatalf("run() admin room members add error = %v", err)
		}
	})

	client, err := moltnetclient.New(clientconfig.AttachmentConfig{
		BaseURL: "http://" + addr,
		Auth:    clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: "operator-secret"},
	})
	if err != nil {
		t.Fatalf("build admin client: %v", err)
	}
	before, err := client.GetRoom(context.Background(), "chat")
	if err != nil {
		t.Fatalf("GetRoom(chat) before restart error = %v", err)
	}
	if len(before.Members) != 1 || before.Members[0] != "alice" {
		t.Fatalf("chat members before restart = %#v, want [alice]", before.Members)
	}

	// Simulate a restart: tear down the first server/app entirely (releasing
	// the sqlite file) and build a completely fresh one from the same,
	// now-rewritten config path.
	server.Close()
	if err := instance.Close(); err != nil {
		t.Fatalf("instance.Close() error = %v", err)
	}

	server2, instance2 := startRestartableMoltnetServer(t, configPath)
	defer server2.Close()
	defer func() { _ = instance2.Close() }()

	after, err := client.GetRoom(context.Background(), "chat")
	if err != nil {
		t.Fatalf("GetRoom(chat) after restart error = %v", err)
	}
	if len(after.Members) != 1 || after.Members[0] != "alice" {
		t.Fatalf("chat members after restart = %#v, want [alice] (membership must survive a restart)", after.Members)
	}
}

// TestAdminRoomMembersWritebackNeverTouchesUnrelatedLocalConfig is F4's
// required regression, matching the second reviewer's live two-server
// reproduction: a `.moltnet/config.json` client config (discovered with
// neither --base-url nor --config passed) resolves the live PATCH to a
// "remote" server, while an entirely unrelated local Moltnet server config
// also happens to sit in cwd (as `./Moltnet` would for a different,
// "livenet" network). Before F4, adminRoomMembersConfigWriteback ignored how
// the live request actually resolved and independently re-derived a "local"
// config purely from --network (empty here) and cwd discovery, silently
// mutating that unrelated file. resolveAdminClientResolution's
// LocalServerConfigPath now stays empty whenever a client config resolved
// the request, so the writeback must not touch anything.
func TestAdminRoomMembersWritebackNeverTouchesUnrelatedLocalConfig(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())

	var patchedPaths []string
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		patchedPaths = append(patchedPaths, r.URL.Path)
		_ = json.NewEncoder(w).Encode(protocol.Room{ID: "chat", Members: []string{"injected:member2"}})
	}))
	defer remoteServer.Close()

	// The client config: one attachment, pointed at the remote server above,
	// for network "remotenet" -- exactly what an operator would have lying
	// around after attaching an agent to a friend's network.
	clientConfigDir := filepath.Join(directory, ".moltnet")
	if err := os.MkdirAll(clientConfigDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", clientConfigDir, err)
	}
	clientConfig := clientconfig.Config{
		Version: clientconfig.VersionV1,
		Agent:   clientconfig.AgentConfig{Name: "op", Runtime: "test"},
		Attachments: []clientconfig.AttachmentConfig{{
			BaseURL:   remoteServer.URL,
			NetworkID: "remotenet",
			MemberID:  "opclient",
			Auth:      clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: "remote-secret"},
		}},
	}
	if err := writeClientConfig(filepath.Join(clientConfigDir, "config.json"), clientConfig); err != nil {
		t.Fatalf("writeClientConfig(): %v", err)
	}

	// An entirely unrelated local server config for a *different* network,
	// "livenet", discoverable via app.ResolveConfigPath's cwd fallback
	// (./Moltnet) -- exactly the file the pre-F4 bug silently mutated.
	localServerPath := filepath.Join(directory, "Moltnet")
	localServerBody := `version: moltnet.v1

network:
  id: livenet
  name: "Live Net"

auth:
  mode: bearer
  tokens:
    - id: operator
      value: livenet-operator-secret
      scopes: [observe, write, admin]

rooms:
  - id: chat
    federation: none
    members: [alice]

pairings: []
`
	if err := os.WriteFile(localServerPath, []byte(localServerBody), 0o600); err != nil {
		t.Fatalf("write local server config: %v", err)
	}
	before, err := os.ReadFile(localServerPath)
	if err != nil {
		t.Fatalf("read local server config before: %v", err)
	}

	stdoutOutput, stderrOutput := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{
			"admin", "room", "members", "add",
			"--room", "chat",
			"--member", "injected:member2",
		}, "test"); err != nil {
			t.Fatalf("run() admin room members add error = %v", err)
		}
	})

	if len(patchedPaths) != 1 || patchedPaths[0] != "/v1/rooms/chat/members" {
		t.Fatalf("expected exactly one PATCH to the remote server, got %#v", patchedPaths)
	}

	// P1-2 (final-gate review): the "no local config file for this
	// connection" aftercare warning must land on stderr, never stdout --
	// exactly so a caller piping "moltnet admin room members add ... | jq"
	// still gets clean, decodable JSON on stdout. Decoding here (rather
	// than substring-matching for the warning's absence) means a future
	// regression that puts any stray text back on stdout fails this test
	// outright instead of silently passing a "does not contain warning:"
	// check against output that happens to still parse.
	var decoded protocol.Room
	if err := json.Unmarshal([]byte(stdoutOutput), &decoded); err != nil {
		t.Fatalf("stdout was not clean JSON (%v): %q", err, stdoutOutput)
	}
	if !strings.Contains(stderrOutput, "warning:") || !strings.Contains(stderrOutput, "no local config file") {
		t.Fatalf("expected the aftercare warning on stderr, got %q", stderrOutput)
	}

	after, err := os.ReadFile(localServerPath)
	if err != nil {
		t.Fatalf("read local server config after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("unrelated local server config %q was mutated by a writeback that should never have touched it:\nbefore:\n%s\nafter:\n%s",
			localServerPath, before, after)
	}
	if strings.Contains(string(after), "injected:member2") {
		t.Fatalf("the remote-only member leaked into the unrelated local server config:\n%s", after)
	}
}
