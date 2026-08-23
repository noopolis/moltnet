package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// writeRoomJoinClientConfig writes a real .moltnet/config.json-shaped agent
// client config at path, the same file `moltnet register-agent` writes
// (register_agent.go's writeAgentTokenToConfig), so runRoomJoin's default
// (non-fallback) resolution path (resolveClientForMember) reads a genuine
// agent identity and bearer token from disk exactly like the real flow.
func writeRoomJoinClientConfig(t *testing.T, path, baseURL, networkID, memberID, agentToken string) {
	t.Helper()
	config := clientconfig.Config{
		Version: clientconfig.VersionV1,
		Agent:   clientconfig.AgentConfig{Name: memberID},
		Attachments: []clientconfig.AttachmentConfig{{
			AgentName: memberID,
			Auth:      clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: agentToken},
			BaseURL:   baseURL,
			MemberID:  memberID,
			NetworkID: networkID,
		}},
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("encode client config: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// registerRoomJoinTestAgent registers agentID against the real server at
// baseURL under open registration (roomCreateTestConfigBody sets
// auth.agent_registration: open) and returns the minted agent token —
// exactly the "agent-token:"-prefixed credential join.go's
// joinActorAuthorized requires, which a plain auth.tokens[] config entry can
// never produce (NewStaticClaims never sets that prefix).
func registerRoomJoinTestAgent(t *testing.T, baseURL, agentID string) string {
	t.Helper()
	client, err := moltnetclient.New(clientconfig.AttachmentConfig{BaseURL: baseURL})
	if err != nil {
		t.Fatalf("build registration client: %v", err)
	}
	registration, err := client.RegisterAgent(context.Background(), protocol.RegisterAgentRequest{RequestedAgentID: agentID})
	if err != nil {
		t.Fatalf("RegisterAgent(%q): %v", agentID, err)
	}
	if registration.AgentToken == "" {
		t.Fatalf("expected an issued agent token for %q under open registration, got %#v", agentID, registration)
	}
	return registration.AgentToken
}

// TestRoomJoinSurvivesRestart is PLAN.md's required restart-survival test:
// create a room with a credential, restart the server (rebuilding a fresh
// internal/app.App from the same config file — the exact failure mode an
// in-memory-only credential map produces), and confirm join still succeeds.
// The credential's sole durable source is rooms[].credential
// (internal/rooms/reconcile.go via app.New's startup ApplyConfigContext
// call), so this fails if `room create` ever regresses to a route-only
// POST /v1/rooms that only ever lands the credential in the process-local
// map PLAN.md 7A.1 warns against.
func TestRoomJoinSurvivesRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, roomCreateTestConfigBody("chat-net", addr, "operator-secret", "  []"))

	server := startRealMoltnetServer(t, configPath)

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"room", "create", "research",
			"--network", "chat-net",
			"--credential", "restart-survival-secret",
		}, "test"); err != nil {
			t.Fatalf("run() room create error = %v", err)
		}
	})

	// Simulate a restart: close the server and rebuild a fresh
	// internal/app.App from the same config file on disk, exactly like
	// console_e2e_test.go's restartingServiceRunner does for a real
	// launchd/systemd restart. Agent registration is a separate, in-memory
	// concern that this test does not exercise across the restart — it
	// happens fresh against the restarted server below — so the only thing
	// this test asserts survived the restart is the room and its
	// credential, both of which are only ever durable via rooms[] in the
	// config file (internal/rooms/reconcile.go, applied again by app.New's
	// own startup ApplyConfigContext call).
	server.Close()
	server = startRealMoltnetServer(t, configPath)
	defer server.Close()

	agentToken := registerRoomJoinTestAgent(t, "http://"+addr, "writer")

	clientConfigPath := filepath.Join(home, "agent-workspace", ".moltnet", "config.json")
	writeRoomJoinClientConfig(t, clientConfigPath, "http://"+addr, "chat-net", "writer", agentToken)

	t.Setenv("ROOM_JOIN_CREDENTIAL", "restart-survival-secret")
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"room", "join", "research",
			"--config", clientConfigPath,
			"--credential-env", "ROOM_JOIN_CREDENTIAL",
		}, "test"); err != nil {
			t.Fatalf("run() room join error = %v", err)
		}
	})
	if !strings.Contains(output, `"id": "research"`) {
		t.Fatalf("unexpected room join output %q", output)
	}
}

// TestRoomJoinRefusedForPairScopedToken confirms a pair-scoped token — the
// credential a peer network holds, never agent-restricted — cannot join a
// room even when handed to `room join` explicitly via --base-url, covering
// both PLAN.md 7A.3's "refused for a pair-scoped token" requirement and the
// advertised-scope fix from the CLI's own vantage point (internal/transport
// covers the same fix at the HTTP layer directly).
func TestRoomJoinRefusedForPairScopedToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	body := fmt.Sprintf(`version: moltnet.v1

network:
  id: chat-net
  name: chat-net Moltnet

server:
  listen_addr: %q

storage:
  kind: memory

auth:
  mode: bearer
  agent_registration: open
  tokens:
    - id: operator
      value: "operator-secret"
      scopes: [observe, write, admin]
    - id: friend-net
      value: "friend-net-secret"
      scopes: [pair]

rooms: []
pairings: []
`, addr)
	writeRoomCreateTestConfig(t, configPath, body)

	server := startRealMoltnetServer(t, configPath)
	defer server.Close()

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"room", "create", "research",
			"--network", "chat-net",
			"--credential", "pair-refusal-secret",
		}, "test"); err != nil {
			t.Fatalf("run() room create error = %v", err)
		}
	})

	t.Setenv("ROOM_JOIN_CREDENTIAL", "pair-refusal-secret")
	t.Setenv("PAIR_TOKEN", "friend-net-secret")
	err := run(context.Background(), []string{
		"room", "join", "research",
		"--base-url", "http://" + addr,
		"--member", "friend-agent",
		"--token-env", "PAIR_TOKEN",
		"--credential-env", "ROOM_JOIN_CREDENTIAL",
	}, "test")
	if err == nil {
		t.Fatal("expected room join to be refused for a pair-scoped token")
	}
}
