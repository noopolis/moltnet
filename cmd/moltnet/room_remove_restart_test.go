package main

import (
	"context"
	"path/filepath"
	"testing"
)

// TestRoomRemoveConfigWritebackSurvivesRestart is P0-1's required
// regression test: create a room via `room create`, remove it via
// `room remove`, then restart the app from the same on-disk (sqlite-backed)
// config and assert the network actually starts.
//
// Before room_remove_writeback.go's fix, this failed at the final
// startRestartableMoltnetServer call: store.RemoveRoomContext's soft delete
// (internal/store/sql_store.go) tombstones the room's primary key without
// freeing it, and `room remove` left the room's rooms[] entry in config
// untouched, so the next app.New()'s applyRequestFromConfig replayed
// CreateRoomContext straight into that tombstoned id -- a duplicate the
// fallback ReconcileRoomContext can never resolve (it only finds live
// rows) -- and app.New() itself returned an error, which
// startRestartableMoltnetServer surfaces via t.Fatalf. Storage is sqlite,
// not memory, so the second app.New() below genuinely reloads state from
// disk rather than reusing the first instance's in-memory store.
func TestRoomRemoveConfigWritebackSurvivesRestart(t *testing.T) {
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

rooms: []

pairings: []
`)

	server, instance := startRestartableMoltnetServer(t, configPath)

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"room", "create", "chat",
			"--network", "chat-net",
		}, "test"); err != nil {
			t.Fatalf("run() room create error = %v", err)
		}
	})

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"room", "remove",
			"--network", "chat-net",
			"--room", "chat",
		}, "test"); err != nil {
			t.Fatalf("run() room remove error = %v", err)
		}
	})

	// Simulate a restart: tear down the first server/app entirely (releasing
	// the sqlite file) and build a completely fresh one from the same,
	// now-rewritten config path. Before the fix, this is exactly where the
	// network failed to start.
	server.Close()
	if err := instance.Close(); err != nil {
		t.Fatalf("instance.Close() error = %v", err)
	}

	server2, instance2 := startRestartableMoltnetServer(t, configPath)
	defer server2.Close()
	defer func() { _ = instance2.Close() }()
}
