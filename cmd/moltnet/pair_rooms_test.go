package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func findRoom(t *testing.T, config app.Config, id string) app.RoomConfig {
	t.Helper()
	for _, room := range config.Rooms {
		if room.ID == id {
			return room
		}
	}
	t.Fatalf("room %q not found in %#v", id, config.Rooms)
	return app.RoomConfig{}
}

func TestRunPairInviteCreatesSharedRoomWithFederation(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	if err := run(context.Background(), []string{
		"pair", "invite",
		"--relay-url", "wss://moltnet-relay.acme.workers.dev",
		"--relay-token-env", "TEST_RELAY_TOKEN",
		"--id", "friend-net",
		"--room", "chat",
		"--config", path,
	}, "test"); err != nil {
		t.Fatalf("run() pair invite error = %v", err)
	}

	config, err := app.LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	room := findRoom(t, config, "chat")
	if !protocol.RoomFederationAllows(room.Federation, "friend-net") {
		t.Fatalf("chat room federation does not allow the new pairing: %#v", room.Federation)
	}
}

func TestRunPairInviteRejectsInvalidRoomID(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config before: %v", err)
	}

	err = run(context.Background(), []string{
		"pair", "invite",
		"--relay-url", "wss://moltnet-relay.acme.workers.dev",
		"--relay-token-env", "TEST_RELAY_TOKEN",
		"--id", "friend-net",
		"--room", "bad room id!",
		"--config", path,
	}, "test")
	if err == nil {
		t.Fatal("expected error for invalid --room id")
	}
	if !strings.Contains(err.Error(), "bad room id!") {
		t.Fatalf("expected error to name the bad id, got %q", err.Error())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("pair invite wrote to config despite invalid --room id:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRunPairInviteAcceptsRepeatedAndCommaSeparatedRooms(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	if err := run(context.Background(), []string{
		"pair", "invite",
		"--relay-url", "wss://moltnet-relay.acme.workers.dev",
		"--relay-token-env", "TEST_RELAY_TOKEN",
		"--id", "friend-net",
		"--room", "chat,news",
		"--room", "alerts",
		"--config", path,
	}, "test"); err != nil {
		t.Fatalf("run() pair invite error = %v", err)
	}

	config, err := app.LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	for _, id := range []string{"chat", "news", "alerts"} {
		room := findRoom(t, config, id)
		if !protocol.RoomFederationAllows(room.Federation, "friend-net") {
			t.Fatalf("room %q federation does not allow the new pairing: %#v", id, room.Federation)
		}
	}
}

func TestRunPairJoinCreatesSharedRoomWithFederation(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	aliceDir := t.TempDir()
	alicePath := writeMoltnetConfig(t, aliceDir, "alice-net", "Alice's Moltnet")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--room", "chat",
			"--config", alicePath,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	bobDir := t.TempDir()
	bobPath := writeMoltnetConfig(t, bobDir, "bob-net", "Bob's Moltnet")
	if err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test"); err != nil {
		t.Fatalf("run() pair <code> error = %v", err)
	}

	bobConfig, err := app.LoadConfigForPath(bobPath, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	room := findRoom(t, bobConfig, "chat")
	if !protocol.RoomFederationAllows(room.Federation, "friend-net") {
		t.Fatalf("mirrored chat room federation does not allow the pairing: %#v", room.Federation)
	}
}

func TestRunPairInviteForceReplacesPairingWithoutDuplicates(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	directory := t.TempDir()
	path := writeMoltnetConfig(t, directory, "alice-net", "Alice's Moltnet")

	inviteArgs := []string{
		"pair", "invite",
		"--relay-url", "wss://moltnet-relay.acme.workers.dev",
		"--relay-token-env", "TEST_RELAY_TOKEN",
		"--id", "friend-net",
		"--room", "chat",
		"--config", path,
	}
	if err := run(context.Background(), inviteArgs, "test"); err != nil {
		t.Fatalf("first run() pair invite error = %v", err)
	}

	// Without --force, re-running with the same --id must refuse, and the
	// hint must mention --force exactly once (not duplicated with the base
	// writeback error's own "(rerun with --force to overwrite)" hint) while
	// also pointing at --id as an alternative.
	err := run(context.Background(), inviteArgs, "test")
	if err == nil {
		t.Fatal("expected error re-running pair invite with the same --id and no --force")
	}
	hintCount := strings.Count(err.Error(), "--force")
	if hintCount != 1 {
		t.Fatalf("expected exactly one --force mention in the error, got %d: %q", hintCount, err.Error())
	}
	if !strings.Contains(err.Error(), "--id") {
		t.Fatalf("expected the pair invite duplicate error to mention --id, got %q", err.Error())
	}

	forceArgs := append(append([]string{}, inviteArgs...), "--force")
	if err := run(context.Background(), forceArgs, "test"); err != nil {
		t.Fatalf("run() pair invite --force error = %v", err)
	}

	config, err := app.LoadConfigForPath(path, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(config.Pairings) != 1 {
		t.Fatalf("expected --force to keep a single pairing, got %#v", config.Pairings)
	}
	if len(config.Auth.Tokens) != 1 {
		t.Fatalf("expected --force to keep a single auth token, got %#v", config.Auth.Tokens)
	}
	room := findRoom(t, config, "chat")
	if room.Federation == nil || len(room.Federation.Pairings) != 1 {
		t.Fatalf("expected --force to keep a single federation entry, got %#v", room.Federation)
	}
}

func TestRunPairJoinForceDoesNotDuplicateFederationOrToken(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	aliceDir := t.TempDir()
	alicePath := writeMoltnetConfig(t, aliceDir, "alice-net", "Alice's Moltnet")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--room", "chat",
			"--config", alicePath,
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	bobDir := t.TempDir()
	bobPath := writeMoltnetConfig(t, bobDir, "bob-net", "Bob's Moltnet")
	if err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test"); err != nil {
		t.Fatalf("first run() pair <code> error = %v", err)
	}

	// The duplicate-without-force error must not repeat the "rerun with
	// --force to overwrite" hint that WritePairing already attaches.
	err := run(context.Background(), []string{"pair", code, "--config", bobPath}, "test")
	if err == nil {
		t.Fatal("expected error re-running pair <code> without --force")
	}
	if hintCount := strings.Count(err.Error(), "rerun with --force"); hintCount != 1 {
		t.Fatalf("expected exactly one \"rerun with --force\" hint, got %d: %q", hintCount, err.Error())
	}

	if err := run(context.Background(), []string{"pair", code, "--config", bobPath, "--force"}, "test"); err != nil {
		t.Fatalf("run() pair <code> --force error = %v", err)
	}

	bobConfig, err := app.LoadConfigForPath(bobPath, "1.0.0")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}
	if len(bobConfig.Auth.Tokens) != 1 {
		t.Fatalf("expected --force to keep a single auth token, got %#v", bobConfig.Auth.Tokens)
	}
	room := findRoom(t, bobConfig, "chat")
	if room.Federation == nil || len(room.Federation.Pairings) != 1 {
		t.Fatalf("expected --force to keep a single federation entry, got %#v", room.Federation)
	}
}
