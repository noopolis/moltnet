package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// This file holds P2-3's regression tests, split out of room_create_test.go
// once that file was approaching the repo's 400-line limit: `room create`'s
// --federation parsing/validation is its own concern from the room-create/
// live-room-preflight tests living there, and both halves of the fix
// (parseRoomFederationFlag's case-fold, validateRoomFederationPairings'
// membership check) are exercised here. writeRoomCreateTestConfig and
// roomCreateTestConfigBody are shared fixtures defined in
// room_create_test.go.

// TestParseRoomFederationFlagCaseFoldsNoneAndAll is P2-3's unit-level
// regression test for the reviewer's probe: before the fix, "All" and
// "NONE" (and any other differently-cased spelling) fell through the exact
// string switch into the comma-separated-pairing-list branch, becoming
// {Mode: list, Pairings: ["All"]} / {Mode: list, Pairings: ["NONE"]} with
// err=<nil> instead of the scalar stance the operator meant.
func TestParseRoomFederationFlagCaseFoldsNoneAndAll(t *testing.T) {
	cases := []struct {
		input    string
		wantMode string
	}{
		{"none", protocol.RoomFederationNone},
		{"NONE", protocol.RoomFederationNone},
		{"None", protocol.RoomFederationNone},
		{"all", protocol.RoomFederationAll},
		{"All", protocol.RoomFederationAll},
		{"ALL", protocol.RoomFederationAll},
	}
	for _, testCase := range cases {
		federation, err := parseRoomFederationFlag(testCase.input)
		if err != nil {
			t.Fatalf("parseRoomFederationFlag(%q) error = %v", testCase.input, err)
		}
		if federation.Mode != testCase.wantMode {
			t.Fatalf("parseRoomFederationFlag(%q) = %#v, want Mode %q", testCase.input, federation, testCase.wantMode)
		}
		if len(federation.Pairings) != 0 {
			t.Fatalf("parseRoomFederationFlag(%q) = %#v, want no pairings for a scalar stance", testCase.input, federation)
		}
	}
}

// TestParseRoomFederationFlagTreatsTypoAsPairingList confirms a value that
// is not a case-insensitive match for "none"/"all" -- a genuine typo like
// "nonw", not just a differently-cased "none" -- still becomes a
// single-pairing federation list, exactly as documented: shape-validating
// that list is parseRoomFederationFlag's job; whether "nonw" actually names
// a configured pairing is validateRoomFederationPairings's job, checked
// separately once a config is available.
func TestParseRoomFederationFlagTreatsTypoAsPairingList(t *testing.T) {
	federation, err := parseRoomFederationFlag("nonw")
	if err != nil {
		t.Fatalf("parseRoomFederationFlag(%q) error = %v", "nonw", err)
	}
	if federation.Mode != protocol.RoomFederationList || len(federation.Pairings) != 1 || federation.Pairings[0] != "nonw" {
		t.Fatalf("parseRoomFederationFlag(%q) = %#v, want a one-entry pairing list", "nonw", federation)
	}
}

// TestValidateRoomFederationPairingsRejectsUnknownID is P2-3's unit-level
// test for the other half of the fix: a federation list naming a pairing id
// this network has never configured must be a clear error naming the valid
// ids, not silently accepted.
func TestValidateRoomFederationPairingsRejectsUnknownID(t *testing.T) {
	federation := &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"nonw"}}
	pairings := []protocol.Pairing{{ID: "friend"}, {ID: "ally"}}

	err := validateRoomFederationPairings(federation, pairings)
	if err == nil {
		t.Fatal("expected an error for a federation pairing id that is not configured")
	}
	if !strings.Contains(err.Error(), "nonw") || !strings.Contains(err.Error(), "friend") || !strings.Contains(err.Error(), "ally") {
		t.Fatalf("error %q should name both the unknown id and the valid configured ids", err.Error())
	}
}

// TestValidateRoomFederationPairingsAcceptsConfiguredID confirms a
// federation list naming an id this network really has configured is a
// no-op -- the fix must not reject valid pairing ids.
func TestValidateRoomFederationPairingsAcceptsConfiguredID(t *testing.T) {
	federation := &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"friend"}}
	pairings := []protocol.Pairing{{ID: "friend"}}

	if err := validateRoomFederationPairings(federation, pairings); err != nil {
		t.Fatalf("validateRoomFederationPairings() = %v, want nil for a configured id", err)
	}
}

// TestRoomCreateRejectsFederationNamingUnknownPairing is P2-3's end-to-end
// regression test, reproducing the reviewer's probe through the real "room
// create" entry point: --federation nonw against a config with no pairings
// configured must be a clear error, and the room must never land in config
// either -- an operator gets a room federating to a pairing that does not
// exist, reported as success, is exactly the bug this closes.
func TestRoomCreateRejectsFederationNamingUnknownPairing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "chat-net", "Moltnet")
	writeRoomCreateTestConfig(t, configPath, roomCreateTestConfigBody("chat-net", "127.0.0.1:1", "operator-secret", "  []"))

	err := run(context.Background(), []string{
		"room", "create", "chat",
		"--network", "chat-net",
		"--federation", "nonw",
	}, "test")
	if err == nil {
		t.Fatal("expected room create to reject --federation naming an unconfigured pairing id")
	}
	if !strings.Contains(err.Error(), "unknown pairing id") {
		t.Fatalf("unexpected error = %v, want it to name the unknown pairing id", err)
	}

	config, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after rejected room create = %v", err)
	}
	if len(config.Rooms) != 0 {
		t.Fatalf("config rooms[] = %#v, want no write from the rejected create", config.Rooms)
	}
}
