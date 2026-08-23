package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestRunReadSinceLastFirstUseThenDelta is PLAN.md 7C.2's core end-to-end
// scenario: the first --since-last call has no stored cursor yet and gets
// the plain unfiltered window (identical to `read` without the flag); it
// persists a cursor from what it saw; the second call sends that cursor as
// PageRequest.After and only shows what the fake server reports as "new."
func TestRunReadSinceLastFirstUseThenDelta(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	var requests []*http.Request
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/rooms/general/messages" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		requests = append(requests, request.Clone(request.Context()))

		if request.URL.Query().Get("after") == "" {
			// First call: the whole current window.
			_ = json.NewEncoder(response).Encode(protocol.MessagePage{
				Messages: []protocol.Message{{ID: "msg_1"}, {ID: "msg_2"}},
			})
			return
		}
		// Second call: only what came after msg_2.
		if request.URL.Query().Get("after") != "msg_2" {
			t.Fatalf("unexpected after cursor %q", request.URL.Query().Get("after"))
		}
		_ = json.NewEncoder(response).Encode(protocol.MessagePage{
			Messages: []protocol.Message{{ID: "msg_3"}},
		})
	}))
	defer server.Close()
	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	firstOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{"read", "--target", "room:general", "--since-last"}, "test"); err != nil {
			t.Fatalf("run() read --since-last (first) error = %v", err)
		}
	})
	if !strings.Contains(firstOutput, `"msg_1"`) || !strings.Contains(firstOutput, `"msg_2"`) {
		t.Fatalf("first --since-last output = %q, want the unfiltered window", firstOutput)
	}

	cursorBytes, err := os.ReadFile(filepath.Join(workspace, ".moltnet", readCursorFileName))
	if err != nil {
		t.Fatalf("read cursor file after first use: %v", err)
	}
	if !strings.Contains(string(cursorBytes), "msg_2") {
		t.Fatalf("cursor file after first use = %s, want it to record msg_2 (the newest message shown)", cursorBytes)
	}

	secondOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{"read", "--target", "room:general", "--since-last"}, "test"); err != nil {
			t.Fatalf("run() read --since-last (second) error = %v", err)
		}
	})
	if !strings.Contains(secondOutput, `"msg_3"`) || strings.Contains(secondOutput, `"msg_1"`) {
		t.Fatalf("second --since-last output = %q, want only msg_3", secondOutput)
	}

	if len(requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(requests))
	}
	if requests[0].URL.Query().Get("after") != "" {
		t.Fatalf("first request after=%q, want empty (no cursor yet)", requests[0].URL.Query().Get("after"))
	}
}

// TestRunReadSinceLastCursorDoesNotAdvanceOnError is PLAN.md 7C.2's "advances
// on success only" rule: a fetch that fails must leave any previously stored
// cursor untouched, so a retry after fixing whatever failed resumes from the
// same place rather than silently skipping messages.
func TestRunReadSinceLastCursorDoesNotAdvanceOnError(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if fail {
			response.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(response).Encode(map[string]string{"error": "boom"})
			return
		}
		_ = json.NewEncoder(response).Encode(protocol.MessagePage{
			Messages: []protocol.Message{{ID: "msg_1"}},
		})
	}))
	defer server.Close()
	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	captureStdout(t, func() {
		if err := run(context.Background(), []string{"read", "--target", "room:general", "--since-last"}, "test"); err != nil {
			t.Fatalf("run() read --since-last (success) error = %v", err)
		}
	})
	cursorPath := filepath.Join(workspace, ".moltnet", readCursorFileName)
	before, err := os.ReadFile(cursorPath)
	if err != nil {
		t.Fatalf("read cursor file after success: %v", err)
	}

	fail = true
	if err := run(context.Background(), []string{"read", "--target", "room:general", "--since-last"}, "test"); err == nil {
		t.Fatal("run() read --since-last with a failing server = nil error, want an error")
	}

	after, err := os.ReadFile(cursorPath)
	if err != nil {
		t.Fatalf("read cursor file after failure: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("cursor file changed after a failed read: before = %s, after = %s", before, after)
	}
}

// TestRunReadSinceLastStaleCursorFallsBackToFreshWindow is PLAN.md 7C.2's
// documented "cursor older than retained history" behavior: a stored cursor
// the server rejects as invalid (message pruned, or otherwise gone) must not
// hard-fail the command -- it falls back to a fresh, unfiltered read and
// persists a new, valid cursor from that.
func TestRunReadSinceLastStaleCursorFallsBackToFreshWindow(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("after") == "stale_msg" {
			response.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(response).Encode(map[string]string{"error": `invalid cursor "stale_msg"`})
			return
		}
		_ = json.NewEncoder(response).Encode(protocol.MessagePage{
			Messages: []protocol.Message{{ID: "msg_fresh"}},
		})
	}))
	defer server.Close()
	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cursorPath := filepath.Join(workspace, ".moltnet", readCursorFileName)
	identity := readCursorIdentity{path: cursorPath, keyPrefix: "agent:local_lab:alpha"}
	target := targetRef{kind: protocol.TargetKindRoom, id: "general"}
	if err := advanceReadCursor(cursorPath, readCursorKey(identity, target), "stale_msg"); err != nil {
		t.Fatalf("seed stale cursor: %v", err)
	}

	output, stderrOutput := captureStdoutAndStderr(t, func() {
		if err := run(context.Background(), []string{"read", "--target", "room:general", "--since-last"}, "test"); err != nil {
			t.Fatalf("run() read --since-last (stale cursor) error = %v", err)
		}
	})
	if !strings.Contains(output, `"msg_fresh"`) {
		t.Fatalf("output = %q, want the fresh fallback window", output)
	}
	// F2: the self-heal must be visible on stdout too (not only the stderr
	// note below), since --since-last's stated audience is an agent parsing
	// stdout, which cannot distinguish a clean delta from a gap otherwise.
	if !strings.Contains(output, `"cursor_reset": true`) {
		t.Fatalf("output = %q, want a cursor_reset marker on the stale-cursor fallback", output)
	}
	wantNote := `note: stored --since-last cursor for room:general is older than retained history; showing the latest messages instead`
	if !strings.Contains(stderrOutput, wantNote) {
		t.Fatalf("stderr = %q, want it to contain %q", stderrOutput, wantNote)
	}

	stored, err := lookupReadCursor(cursorPath, readCursorKey(identity, target))
	if err != nil {
		t.Fatalf("lookupReadCursor() error = %v", err)
	}
	if stored != "msg_fresh" {
		t.Fatalf("stored cursor after stale fallback = %q, want %q", stored, "msg_fresh")
	}
}

// TestRunReadSinceLastRefusesBaseURLOverride checks the CLI-level refusal:
// --since-last with --base-url (no locally resolvable config to anchor a
// cursor file to) must fail clearly rather than silently skip persistence.
func TestRunReadSinceLastRefusesBaseURLOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		t.Fatalf("unexpected request %s %s: --since-last should have been refused before any request", request.Method, request.URL.Path)
	}))
	defer server.Close()

	err := run(context.Background(), []string{
		"read", "--target", "room:general", "--since-last", "--base-url", server.URL,
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "--base-url") {
		t.Fatalf("run() read --since-last --base-url error = %v, want a clear --base-url refusal", err)
	}
}
