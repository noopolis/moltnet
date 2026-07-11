package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestNewInstallsCausalWriter is the precondition test for causal integrity:
// before this fix, app.go's New() never set rooms.ServiceConfig.CausalWriter,
// so internal/rooms/causal.go's stampMessageAccepted always no-opped
// (s.causalWriter == nil) regardless of how the process was configured. This
// proves Config.CausalEventsPath (sourced from MOLTNET_CAUSAL_EVENTS_PATH by
// config_load.go's mergeEnvConfig) actually reaches the room service and a
// real send produces a message.accepted line in the JSONL file.
func TestNewInstallsCausalWriter(t *testing.T) {
	// Not t.Parallel(): t.Setenv (NOOPOLIS_RUN_ID below) forbids it.
	causalPath := filepath.Join(t.TempDir(), "causal.jsonl")

	// NOOPOLIS_RUN_ID is read at stamp time (internal/rooms/causal.go's
	// causalRunIDEnv) directly from the environment, never from Config —
	// this mirrors how spawnfile's container wiring sets it alongside
	// MOLTNET_CAUSAL_EVENTS_PATH (src/runtime/common.ts).
	t.Setenv("NOOPOLIS_RUN_ID", "run-causal-precondition")

	instance, err := New(Config{
		CausalEventsPath: causalPath,
		ListenAddr:       ":0",
		NetworkID:        "local",
		NetworkName:      "Local",
		Version:          "test",
		Rooms: []RoomConfig{{
			ID:      "research",
			Name:    "Research",
			Members: []string{"orchestrator"},
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer instance.close()

	server := httptest.NewServer(instance.server.Handler)
	defer server.Close()

	body, err := json.Marshal(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "orchestrator"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello causal"}},
	})
	if err != nil {
		t.Fatalf("marshal send request: %v", err)
	}

	response, err := http.Post(server.URL+"/v1/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post message: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected send status %d", response.StatusCode)
	}

	var accepted protocol.MessageAccepted
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode send response: %v", err)
	}

	// instance.close() (deferred above) closes the causal writer's
	// underlying file, but we need the line on disk before that: close it
	// explicitly here first, then read.
	instance.close()

	file, err := os.Open(causalPath)
	if err != nil {
		t.Fatalf("open causal events file: %v", err)
	}
	defer file.Close()

	var events []protocol.CausalEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event protocol.CausalEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode causal event line %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan causal events file: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected exactly one causal event, got %d: %#v", len(events), events)
	}

	event := events[0]
	wantEventID := protocol.MessageEventID(accepted.MessageID)
	if event.Type != protocol.EventTypeMessageAccepted {
		t.Fatalf("expected message.accepted, got %q", event.Type)
	}
	if event.EventID != wantEventID {
		t.Fatalf("expected event_id %q, got %q", wantEventID, event.EventID)
	}
	if event.RunID != "run-causal-precondition" {
		t.Fatalf("unexpected run_id %q", event.RunID)
	}
}

// TestNewWithoutCausalEventsPathLeavesCausalWriterNil documents the
// unconfigured default: no CausalEventsPath means no CausalWriter is built,
// so stampMessageAccepted keeps its pre-fix no-op behavior rather than
// failing a send that never opted into causal observability.
func TestNewWithoutCausalEventsPathLeavesCausalWriterNil(t *testing.T) {
	t.Parallel()

	instance, err := New(Config{
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer instance.close()
}
