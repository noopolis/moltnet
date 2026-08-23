package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestRoomRemoveMatchesDeprecatedAdminAlias is PLAN.md 7A.0's namespace-move
// gate: "moltnet room remove" and the deprecated "moltnet admin room
// remove" must produce identical results (removeRoomCommand, remove.go, is
// the single shared implementation behind both), and the deprecated alias
// must still work and print a short deprecation note.
func TestRoomRemoveMatchesDeprecatedAdminAlias(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		_ = json.NewEncoder(response).Encode(protocol.RemoveResult{
			Removed: true,
			Kind:    "room",
			ID:      "stale-room",
			Mode:    "soft",
		})
	}))
	defer server.Close()

	t.Setenv("MOLTNET_ADMIN_TOKEN", "admin-secret")

	roomOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"room", "remove",
			"--base-url", server.URL,
			"--room", "stale-room",
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() room remove error = %v", err)
		}
	})

	adminOutput, adminStderr := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{
			"admin", "room", "remove",
			"--base-url", server.URL,
			"--room", "stale-room",
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() admin room remove error = %v", err)
		}
	})

	if roomOutput != adminOutput {
		t.Fatalf("room remove output %q != admin room remove output %q", roomOutput, adminOutput)
	}
	if !strings.Contains(roomOutput, `"kind": "room"`) {
		t.Fatalf("unexpected room remove output %q", roomOutput)
	}
	if !strings.Contains(adminStderr, "deprecated") || !strings.Contains(adminStderr, "moltnet room remove") {
		t.Fatalf("expected admin room remove to print a deprecation note on stderr, got %q", adminStderr)
	}
	if len(requests) != 2 || requests[0] != "DELETE /v1/rooms/stale-room" || requests[1] != "DELETE /v1/rooms/stale-room" {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

// TestRoomRemoveNeverPrintsDeprecationNote asserts the new, non-deprecated
// "moltnet room remove" entry point never prints the deprecation note —
// only the deprecated admin-namespaced spelling should. It no longer
// asserts stderr is entirely empty: P0-1's fix (room_remove_writeback.go)
// makes removeRoomCommand warn on stderr whenever it did not resolve a
// local server config to clean up rooms[] in — exactly this test's own
// shape, an explicit --base-url with no local config anywhere for
// "stale-room" — and that warning is required behavior, not a regression.
func TestRoomRemoveNeverPrintsDeprecationNote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(response).Encode(protocol.RemoveResult{Removed: true, Kind: "room", ID: "stale-room", Mode: "soft"})
	}))
	defer server.Close()

	t.Setenv("MOLTNET_ADMIN_TOKEN", "admin-secret")
	_, stderrOutput := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{
			"room", "remove",
			"--base-url", server.URL,
			"--room", "stale-room",
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() room remove error = %v", err)
		}
	})
	if strings.Contains(stderrOutput, "deprecated") {
		t.Fatalf("expected no deprecation note from the non-deprecated entry point, got %q", stderrOutput)
	}
}
