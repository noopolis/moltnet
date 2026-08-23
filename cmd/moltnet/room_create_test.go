package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// roomCreateTestConfigBody writes a minimal bearer-mode Moltnet config with
// one admin-scoped operator token, in-memory storage, and any seedRooms
// already present in rooms[] — the fixture every room_create_test.go/
// room_create_federation_test.go/room_join_test.go case starts from.
func roomCreateTestConfigBody(networkID, listenAddr, operatorToken string, seedRooms string) string {
	return fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: %q

storage:
  kind: memory

auth:
  mode: bearer
  agent_registration: open
  tokens:
    - id: operator
      value: %q
      scopes: [observe, write, admin]

rooms:
%s
pairings: []
`, networkID, networkID+" Moltnet", listenAddr, operatorToken, seedRooms)
}

func writeRoomCreateTestConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// TestRoomCreateWritesExplicitNoneFederationAndSurvivesRollback exercises
// `room create` end to end against a config with no server running: the
// config write must stand (this is PLAN.md 7A.1's "fall back to a restart
// reminder, never roll back the write, when the daemon is not answering"),
// federation must land as an explicit "none" (never nil), and a pairing
// added afterward must still reload cleanly — the golden test PLAN.md's 7A
// federation rule calls for.
func TestRoomCreateWritesExplicitNoneFederationAndSurvivesRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	// 127.0.0.1:1 is never a listening service: this proves the "daemon not
	// answering" path without racing a real freed port.
	writeRoomCreateTestConfig(t, configPath, roomCreateTestConfigBody("chat-net", "127.0.0.1:1", "operator-secret", "  []"))

	// The restart reminder is on stderr, not stdout (P3): stdout is the
	// success path's JSON, and a caller piping "moltnet room create x | jq"
	// on this fallback path must see nothing else mixed in.
	stdoutOutput, stderrOutput := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{
			"room", "create", "chat",
			"--network", "chat-net",
		}, "test"); err != nil {
			t.Fatalf("run() room create error = %v", err)
		}
	})
	if !strings.Contains(stderrOutput, "not answering") {
		t.Fatalf("expected a restart reminder for the unreachable daemon on stderr, got %q", stderrOutput)
	}
	if strings.Contains(stdoutOutput, "not answering") {
		t.Fatalf("restart reminder leaked onto stdout: %q", stdoutOutput)
	}

	config, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after room create = %v", err)
	}
	if len(config.Rooms) != 1 || config.Rooms[0].ID != "chat" {
		t.Fatalf("expected the room to be durably written, got %#v", config.Rooms)
	}
	if config.Rooms[0].Federation == nil || config.Rooms[0].Federation.Mode != protocol.RoomFederationNone {
		t.Fatalf("room federation = %#v, want an explicit {Mode: none}", config.Rooms[0].Federation)
	}

	// The golden test: this config must still reload cleanly once a pairing
	// is added, which is exactly what a nil federation stance would break.
	pairing := app.PairingWriteback{
		ID:              "friend",
		RemoteNetworkID: "friend-net",
		Token:           "friend-token",
		Relay: &app.PairingRelayWriteback{
			URL:   "wss://relay.example.dev",
			Room:  "room-xyz",
			Token: "relay-secret-token",
		},
	}
	authToken := app.AuthTokenWriteback{ID: "friend", Value: "friend-token", Scopes: []string{"pair"}}
	if err := app.WritePairing(configPath, pairing, authToken, nil, false); err != nil {
		t.Fatalf("WritePairing() after room create = %v", err)
	}
	if _, err := app.LoadConfigForPath(configPath, ""); err != nil {
		t.Fatalf("LoadConfigForPath() after adding a pairing = %v", err)
	}
}

// TestRoomCreateRefusesDuplicateRoomID confirms `room create` surfaces the
// config-level duplicate check as a real command error, never a silent
// overwrite, and never attempts to reach a running service for a write that
// never landed.
func TestRoomCreateRefusesDuplicateRoomID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	writeRoomCreateTestConfig(t, configPath, roomCreateTestConfigBody("chat-net", "127.0.0.1:1", "operator-secret", "  - id: chat\n    federation: none\n"))

	err := run(context.Background(), []string{"room", "create", "chat", "--network", "chat-net"}, "test")
	if err == nil {
		t.Fatal("expected room create to refuse an id already present in rooms[]")
	}
}

// TestRoomCreateAppliesSingleRoomDeltaWithoutWipingOtherMembers is PLAN.md
// 7A.1's P2 gate: creating room B must never clear membership `admin room
// members add` already granted room A, which is exactly what a full-config
// /v1/apply would do (ReconcileRoomContext replaces a room's member list
// wholesale). It runs against a real internal/app.App + real HTTP server,
// not a mock, so the real ApplyConfigContext/CreateRoomContext/
// ReconcileRoomContext path is what is actually being exercised.
func TestRoomCreateAppliesSingleRoomDeltaWithoutWipingOtherMembers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, roomCreateTestConfigBody("chat-net", addr, "operator-secret", "  - id: room-a\n    federation: none\n"))

	server := startRealMoltnetServer(t, configPath)
	defer server.Close()

	// Add alice to room-a the way an operator does today: an in-memory-only
	// PATCH with no config writeback (PLAN.md 7A.1's own description of
	// this step).
	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"admin", "room", "members", "add",
			"--base-url", "http://" + addr,
			"--room", "room-a",
			"--member", "alice",
			"--token", "operator-secret",
		}, "test"); err != nil {
			t.Fatalf("run() admin room members add error = %v", err)
		}
	})

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"room", "create", "room-b",
			"--network", "chat-net",
		}, "test"); err != nil {
			t.Fatalf("run() room create room-b error = %v", err)
		}
	})

	client, err := moltnetclient.New(clientconfig.AttachmentConfig{
		BaseURL: "http://" + addr,
		Auth:    clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: "operator-secret"},
	})
	if err != nil {
		t.Fatalf("build admin client: %v", err)
	}
	roomA, err := client.GetRoom(context.Background(), "room-a")
	if err != nil {
		t.Fatalf("GetRoom(room-a) error = %v", err)
	}
	if len(roomA.Members) != 1 || roomA.Members[0] != "alice" {
		t.Fatalf("room-a members = %#v, want [alice] (room-b's create must not have wiped them)", roomA.Members)
	}

	roomB, err := client.GetRoom(context.Background(), "room-b")
	if err != nil {
		t.Fatalf("GetRoom(room-b) error = %v", err)
	}
	if roomB.ID != "room-b" {
		t.Fatalf("unexpected room-b %#v", roomB)
	}
}

// TestRoomCreateRefusesLiveRoomNotTrackedInConfig is P2-1's regression test,
// reproducing the reviewer's proven scenario exactly: a room already exists
// on the running service (here, via a direct ApplyConfig call standing in
// for POST /v1/rooms, the console, or a separate `moltnet apply` — none of
// which ever touch *this* config file's rooms[]) with real members. Before
// the fix, `moltnet room create` for that same id passed WriteRoom's
// config-only duplicate check, then POSTed a create delta that
// ReconcileRoomContext turned into a wholesale membership replacement —
// members gone, exit code 0, success JSON printed. This asserts the command
// now refuses instead, and that the room's live members and this config's
// rooms[] (which never had the room) are both left exactly as they were.
func TestRoomCreateRefusesLiveRoomNotTrackedInConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, roomCreateTestConfigBody("chat-net", addr, "operator-secret", "  []"))

	server := startRealMoltnetServer(t, configPath)
	defer server.Close()

	adminClient, err := moltnetclient.New(clientconfig.AttachmentConfig{
		BaseURL: "http://" + addr,
		Auth:    clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: "operator-secret"},
	})
	if err != nil {
		t.Fatalf("build admin client: %v", err)
	}

	// Seed room "chat" with real members directly against the running
	// service, exactly like a POST /v1/rooms, a console action, or a
	// different `moltnet apply` would -- none of which write this config
	// file's rooms[], which is the whole reason WriteRoom's own duplicate
	// check cannot see this room coming.
	if _, err := adminClient.ApplyConfig(context.Background(), protocol.ApplyConfigRequest{
		Rooms: []protocol.CreateRoomRequest{{
			ID:      "chat",
			Members: []string{"alice", "bob"},
		}},
	}); err != nil {
		t.Fatalf("seed live room chat: %v", err)
	}

	err = run(context.Background(), []string{
		"room", "create", "chat",
		"--network", "chat-net",
	}, "test")
	if err == nil {
		t.Fatal("expected room create to refuse a room id already live on the running service")
	}
	if !strings.Contains(err.Error(), "already exists on the running service") {
		t.Fatalf("unexpected error = %v, want a live-room refusal", err)
	}

	room, err := adminClient.GetRoom(context.Background(), "chat")
	if err != nil {
		t.Fatalf("GetRoom(chat) after refused create = %v", err)
	}
	if len(room.Members) != 2 || room.Members[0] != "alice" || room.Members[1] != "bob" {
		t.Fatalf("room chat members = %#v, want [alice bob] to survive the refused create", room.Members)
	}

	config, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after refused create = %v", err)
	}
	if len(config.Rooms) != 0 {
		t.Fatalf("config rooms[] = %#v, want no write from the refused create", config.Rooms)
	}
}

// TestRoomCreateWarnsAboutUnverifiedLivenessWhenUnreachable is the interim
// mitigation's regression test for the gap refuseIfRoomAlreadyLive cannot
// cover on its own: when the service is unreachable, runRoomCreate's
// live-room preflight (step 1) never runs at all, so a room id that already
// exists in the persistent store — with real members a different config
// never tracked — gets its config entry written here anyway, and that entry
// becomes the room's whole membership at the next service start
// (applyRequestFromConfig -> ReconcileRoomContext). This asserts the
// restart-reminder fallback now says so on stderr, alongside the existing
// "not answering" reminder, and that stdout stays untouched.
func TestRoomCreateWarnsAboutUnverifiedLivenessWhenUnreachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	// 127.0.0.1:1 is never a listening service: this proves the "daemon not
	// answering" path without racing a real freed port.
	writeRoomCreateTestConfig(t, configPath, roomCreateTestConfigBody("chat-net", "127.0.0.1:1", "operator-secret", "  []"))

	stdoutOutput, stderrOutput := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{
			"room", "create", "chat",
			"--network", "chat-net",
		}, "test"); err != nil {
			t.Fatalf("run() room create error = %v", err)
		}
	})

	if !strings.Contains(stderrOutput, "liveness could not be verified") {
		t.Fatalf("expected an unverified-liveness warning on stderr, got %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "chat") {
		t.Fatalf("expected the unverified-liveness warning to name the room, got %q", stderrOutput)
	}
	if strings.Contains(stdoutOutput, "liveness") {
		t.Fatalf("unverified-liveness warning leaked onto stdout: %q", stdoutOutput)
	}
	if strings.TrimSpace(stdoutOutput) != "" {
		t.Fatalf("expected clean stdout on the unreachable fallback path, got %q", stdoutOutput)
	}
}

// TestRoomCreateOmitsLivenessWarningWhenReachable is
// TestRoomCreateWarnsAboutUnverifiedLivenessWhenUnreachable's counterpart:
// when the service answers, refuseIfRoomAlreadyLive actually runs the
// preflight (runRoomCreate step 1), so the unverified-liveness warning
// would be misleading noise and must not appear.
func TestRoomCreateOmitsLivenessWarningWhenReachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, roomCreateTestConfigBody("chat-net", addr, "operator-secret", "  []"))

	server := startRealMoltnetServer(t, configPath)
	defer server.Close()

	_, stderrOutput := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{
			"room", "create", "chat",
			"--network", "chat-net",
		}, "test"); err != nil {
			t.Fatalf("run() room create error = %v", err)
		}
	})

	if strings.Contains(stderrOutput, "liveness could not be verified") {
		t.Fatalf("unverified-liveness warning should not appear when the service is reachable, got %q", stderrOutput)
	}
}

// TestRoomListIsThinAliasOverRoomsListing confirms `room list` calls the
// same GET /v1/rooms route `conversations` already uses, with no separate
// listing implementation of its own.
func TestRoomListIsThinAliasOverRoomsListing(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestPath = request.Method + " " + request.URL.Path
		_ = json.NewEncoder(response).Encode(protocol.RoomPage{Rooms: []protocol.Room{{ID: "chat"}}})
	}))
	defer server.Close()

	t.Setenv("MOLTNET_ADMIN_TOKEN", "admin-secret")
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"room", "list",
			"--base-url", server.URL,
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() room list error = %v", err)
		}
	})

	if requestPath != "GET /v1/rooms" {
		t.Fatalf("unexpected request %q, want GET /v1/rooms", requestPath)
	}
	if !strings.Contains(output, `"id": "chat"`) {
		t.Fatalf("unexpected room list output %q", output)
	}
}
