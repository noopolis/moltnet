package observability

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestWriteMessageAcceptedFixtureWritesTwoValidRecords(t *testing.T) {
	var buf bytes.Buffer

	if err := WriteMessageAcceptedFixture(&buf, "fixture-run-moltnet"); err != nil {
		t.Fatalf("WriteMessageAcceptedFixture() error = %v", err)
	}

	scanner := bufio.NewScanner(&buf)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d: %v", len(lines), lines)
	}

	var events []protocol.CausalEvent
	for _, line := range lines {
		var event protocol.CausalEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("unmarshal line %q: %v", line, err)
		}
		if err := event.Validate(); err != nil {
			t.Fatalf("decoded line failed Validate(): %v (line=%s)", err, line)
		}
		events = append(events, event)
	}

	first, second := events[0], events[1]

	if first.RunID != "fixture-run-moltnet" || second.RunID != "fixture-run-moltnet" {
		t.Fatalf("expected both events to carry run_id fixture-run-moltnet, got %q and %q", first.RunID, second.RunID)
	}
	if first.EventID != "moltnet:fixture-m1" {
		t.Fatalf("first.EventID = %q, want moltnet:fixture-m1", first.EventID)
	}
	if second.EventID != "moltnet:fixture-m2" {
		t.Fatalf("second.EventID = %q, want moltnet:fixture-m2", second.EventID)
	}
	if first.Emitter.StreamID != "network:fixture-network" || second.Emitter.StreamID != "network:fixture-network" {
		t.Fatalf("expected stream_id network:fixture-network, got %q and %q", first.Emitter.StreamID, second.Emitter.StreamID)
	}
	if first.Emitter.Seq != 1 || second.Emitter.Seq != 2 {
		t.Fatalf("expected contiguous seq 1,2 got %d,%d", first.Emitter.Seq, second.Emitter.Seq)
	}
	if first.Type != protocol.EventTypeMessageAccepted || second.Type != protocol.EventTypeMessageAccepted {
		t.Fatalf("expected both events to be %q, got %q and %q", protocol.EventTypeMessageAccepted, first.Type, second.Type)
	}

	for _, event := range events {
		if event.PrincipalID != "agent:fixture-agent" {
			t.Fatalf("principal_id = %q, want grammar-shaped agent:fixture-agent", event.PrincipalID)
		}
		if event.CauseEventIDs == nil || len(event.CauseEventIDs) != 0 {
			t.Fatalf("expected empty (non-nil) cause_event_ids, got %v", event.CauseEventIDs)
		}
	}

	var firstPayload, secondPayload protocol.MessageAcceptedPayload
	if err := json.Unmarshal(first.Payload, &firstPayload); err != nil {
		t.Fatalf("unmarshal first payload: %v", err)
	}
	if err := json.Unmarshal(second.Payload, &secondPayload); err != nil {
		t.Fatalf("unmarshal second payload: %v", err)
	}
	if firstPayload.MessageID != "fixture-m1" {
		t.Fatalf("first payload.message_id = %q, want fixture-m1", firstPayload.MessageID)
	}
	if secondPayload.MessageID != "fixture-m2" {
		t.Fatalf("second payload.message_id = %q, want fixture-m2", secondPayload.MessageID)
	}
	if firstPayload.PolicyDecision != protocol.PolicyDecisionAccepted || secondPayload.PolicyDecision != protocol.PolicyDecisionAccepted {
		t.Fatalf("expected policy_decision accepted, got %q and %q", firstPayload.PolicyDecision, secondPayload.PolicyDecision)
	}
	if firstPayload.ContentSHA256 == "" || secondPayload.ContentSHA256 == "" {
		t.Fatal("expected non-empty content_sha256 on both payloads")
	}
	if firstPayload.ContentSHA256 == secondPayload.ContentSHA256 {
		t.Fatal("expected distinct content_sha256 for distinct fixture texts")
	}
}

func TestWriteMessageAcceptedFixtureIsDeterministicAcrossRuns(t *testing.T) {
	var first, second bytes.Buffer

	if err := WriteMessageAcceptedFixture(&first, "fixture-run-moltnet"); err != nil {
		t.Fatalf("WriteMessageAcceptedFixture() error = %v", err)
	}
	if err := WriteMessageAcceptedFixture(&second, "fixture-run-moltnet"); err != nil {
		t.Fatalf("WriteMessageAcceptedFixture() error = %v", err)
	}

	if first.String() != second.String() {
		t.Fatalf("expected byte-identical output across runs, got:\n%s\nvs\n%s", first.String(), second.String())
	}
}
