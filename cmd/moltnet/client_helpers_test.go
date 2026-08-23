package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestParseTarget is the PLAN 7C.3 table test for the parser leg: room and
// dm targets keep working exactly as before, thread:<id> now parses
// (roomID left unset — resolveThreadTarget fills it in later), and every
// other shape is still rejected with a clear error.
func TestParseTarget(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantKind  string
		wantID    string
		wantError bool
	}{
		{name: "room", input: "room:general", wantKind: protocol.TargetKindRoom, wantID: "general"},
		{name: "thread", input: "thread:thr_1", wantKind: protocol.TargetKindThread, wantID: "thr_1"},
		{name: "dm", input: "dm:dm_1", wantKind: protocol.TargetKindDM, wantID: "dm_1"},
		{name: "unsupported kind", input: "agent:alpha", wantError: true},
		{name: "missing colon", input: "general", wantError: true},
		{name: "empty id", input: "room:", wantError: true},
		{name: "empty value", input: "", wantError: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			target, err := parseTarget(testCase.input)
			if testCase.wantError {
				if err == nil {
					t.Fatalf("parseTarget(%q) error = nil, want an error", testCase.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTarget(%q) error = %v, want nil", testCase.input, err)
			}
			if target.kind != testCase.wantKind || target.id != testCase.wantID {
				t.Fatalf("parseTarget(%q) = %+v, want kind=%q id=%q", testCase.input, target, testCase.wantKind, testCase.wantID)
			}
			if target.roomID != "" {
				t.Fatalf("parseTarget(%q) roomID = %q, want empty until resolveThreadTarget runs", testCase.input, target.roomID)
			}
		})
	}
}

// TestResolveThreadTargetFetchesRoomID exercises resolveThreadTarget's
// server round trip directly (GetThread), and its no-op passthrough for
// non-thread targets.
func TestResolveThreadTargetFetchesRoomID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/threads/thr_1" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		_ = json.NewEncoder(response).Encode(protocol.Thread{ID: "thr_1", RoomID: "general"})
	}))
	defer server.Close()

	client, err := moltnetclient.New(clientconfig.AttachmentConfig{
		Auth:      clientconfig.AuthConfig{Mode: "none"},
		BaseURL:   server.URL,
		MemberID:  "alpha",
		NetworkID: "local",
	})
	if err != nil {
		t.Fatalf("moltnetclient.New() error = %v", err)
	}

	resolved, err := resolveThreadTarget(context.Background(), client, targetRef{id: "thr_1", kind: protocol.TargetKindThread})
	if err != nil {
		t.Fatalf("resolveThreadTarget() error = %v", err)
	}
	if resolved.roomID != "general" {
		t.Fatalf("resolveThreadTarget() roomID = %q, want %q", resolved.roomID, "general")
	}

	roomTarget := targetRef{id: "general", kind: protocol.TargetKindRoom}
	passthrough, err := resolveThreadTarget(context.Background(), client, roomTarget)
	if err != nil {
		t.Fatalf("resolveThreadTarget() non-thread error = %v", err)
	}
	if passthrough != roomTarget {
		t.Fatalf("resolveThreadTarget() non-thread = %+v, want unchanged %+v", passthrough, roomTarget)
	}
}

// TestResolveThreadTargetErrorsOnMissingRoomID guards the case where the
// server reports a thread record with no room attached at all — a
// same-shape defensive check to unknownRoomError's server-side equivalent,
// but on the client before any authorization decision is made from it.
func TestResolveThreadTargetErrorsOnMissingRoomID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(response).Encode(protocol.Thread{ID: "thr_1"})
	}))
	defer server.Close()

	client, err := moltnetclient.New(clientconfig.AttachmentConfig{
		Auth:      clientconfig.AuthConfig{Mode: "none"},
		BaseURL:   server.URL,
		MemberID:  "alpha",
		NetworkID: "local",
	})
	if err != nil {
		t.Fatalf("moltnetclient.New() error = %v", err)
	}

	if _, err := resolveThreadTarget(context.Background(), client, targetRef{id: "thr_1", kind: protocol.TargetKindThread}); err == nil {
		t.Fatal("resolveThreadTarget() error = nil, want an error for a thread with no room_id")
	}
}

// TestEnsureTargetAllowedThread is the PLAN 7C.3 authorization-rule table
// test: a thread's local allowlist check is exactly its resolved room's
// check (ensureRoomAllowed), reused rather than duplicated. This covers
// both directions — attached and refused — and the "not yet resolved"
// programmer-error guard.
func TestEnsureTargetAllowedThread(t *testing.T) {
	writable := true
	readOnly := false
	attachment := clientconfig.AttachmentConfig{
		MemberID: "alpha",
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "general", CanWrite: &writable},
			{ID: "readonly-room", CanWrite: &readOnly},
		},
	}

	cases := []struct {
		name      string
		target    targetRef
		wantError string
	}{
		{
			name:   "thread in an attached writable room is allowed",
			target: targetRef{id: "thr_1", kind: protocol.TargetKindThread, roomID: "general"},
		},
		{
			name:      "thread in a room the caller cannot access is refused",
			target:    targetRef{id: "thr_2", kind: protocol.TargetKindThread, roomID: "not-attached"},
			wantError: `not attached`,
		},
		{
			name:      "thread in a read-only attached room is refused",
			target:    targetRef{id: "thr_3", kind: protocol.TargetKindThread, roomID: "readonly-room"},
			wantError: "read-only",
		},
		{
			name:      "unresolved thread roomID is a clear internal error, not a false allow",
			target:    targetRef{id: "thr_4", kind: protocol.TargetKindThread},
			wantError: "was not resolved",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ensureTargetAllowed(attachment, testCase.target)
			if testCase.wantError == "" {
				if err != nil {
					t.Fatalf("ensureTargetAllowed(%+v) error = %v, want nil", testCase.target, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("ensureTargetAllowed(%+v) error = %v, want it to contain %q", testCase.target, err, testCase.wantError)
			}
			// The thread id itself must be named in the error, not just the
			// room id, so the caller can tell which of their commands failed.
			if !strings.Contains(err.Error(), testCase.target.id) {
				t.Fatalf("ensureTargetAllowed(%+v) error = %v, want it to name the thread id %q", testCase.target, err, testCase.target.id)
			}
		})
	}
}
