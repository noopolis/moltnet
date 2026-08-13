package protocol

import "testing"

func TestParseCausalEventValidatesCanonicalEnvelopeFields(t *testing.T) {
	raw := map[string]any{
		"version":         CausalEventVersion,
		"run_id":          "run_1",
		"event_id":        "moltnet:e1",
		"emitter":         map[string]any{"system": "moltnet", "stream_id": "network:room-1", "seq": 1},
		"type":            "message.accepted",
		"principal_id":    "agent:writer",
		"recorded_at":     "2026-07-09T00:00:00.000Z",
		"cause_event_ids": []any{},
		"payload":         map[string]any{"message_id": "m1"},
	}
	if _, err := ParseCausalEvent(raw); err != nil {
		t.Fatalf("ParseCausalEvent() error = %v", err)
	}

	raw["event_id"] = "unknown:e1"
	if _, err := ParseCausalEvent(raw); err == nil {
		t.Fatalf("expected unknown-event-system rejection")
	}
}

func TestParseCausalEventCauseIDsUseOpenNamespaces(t *testing.T) {
	raw := map[string]any{
		"version":         CausalEventVersion,
		"run_id":          "run_1",
		"event_id":        "moltnet:e1",
		"emitter":         map[string]any{"system": "moltnet", "stream_id": "network:room-1", "seq": 1},
		"type":            "message.accepted",
		"principal_id":    "agent:writer",
		"recorded_at":     "2026-07-09T00:00:00.000Z",
		"cause_event_ids": []any{"driver:turn:7"},
		"payload":         map[string]any{"message_id": "m1"},
	}
	parsed, err := ParseCausalEvent(raw)
	if err != nil {
		t.Fatalf("expected foreign cause namespace acceptance, got %v", err)
	}
	if len(parsed.CauseEventIDs) != 1 || parsed.CauseEventIDs[0] != "driver:turn:7" {
		t.Fatalf("foreign cause did not survive parsing: %#v", parsed.CauseEventIDs)
	}

	raw["cause_event_ids"] = []any{"fixture-turn-1"}
	if _, err := ParseCausalEvent(raw); err == nil {
		t.Fatal("expected bare cause id rejection")
	}

	raw["cause_event_ids"] = []any{"driver:turn:7", "driver:turn:7"}
	if _, err := ParseCausalEvent(raw); err == nil {
		t.Fatal("expected duplicate cause id rejection")
	}
}

func TestParseCausalStreamFinalValidatesSequence(t *testing.T) {
	raw := map[string]any{
		"version":   CausalStreamFinalVersion,
		"run_id":    "run_1",
		"final_seq": 0,
		"emitter":   map[string]any{"system": "moltnet", "stream_id": "network:empty"},
	}
	if _, err := ParseCausalStreamFinal(raw); err != nil {
		t.Fatalf("ParseCausalStreamFinal() error = %v", err)
	}

	raw["final_seq"] = -1
	if _, err := ParseCausalStreamFinal(raw); err == nil {
		t.Fatal("expected negative final_seq rejection")
	}

	raw["final_seq"] = 1e100
	if _, err := ParseCausalStreamFinal(raw); err == nil {
		t.Fatal("expected unsafe integer rejection")
	}
}

func TestParseCausalDigestDomainEnforcesExpectedTuples(t *testing.T) {
	raw := map[string]any{
		"version":       CausalDigestDomainVersion,
		"label":         CausalDigestLabelCanonicalEvent,
		"hash":          "sha-256",
		"subject_bytes": CausalCanonicalJSONVersion + ":utf-8",
		"output":        "lowercase-hex",
	}
	if _, err := ParseCausalDigestDomain(raw); err != nil {
		t.Fatalf("ParseCausalDigestDomain() error = %v", err)
	}

	raw["label"] = "bogus"
	if _, err := ParseCausalDigestDomain(raw); err == nil {
		t.Fatal("expected unknown label rejection")
	}

	raw["label"] = CausalDigestLabelExactBytes
	raw["subject_bytes"] = "bad-subject"
	if _, err := ParseCausalDigestDomain(raw); err == nil {
		t.Fatal("expected mismatched subject_bytes rejection")
	}
}

func TestParseCausalBundleRejectsCrossRunCausesAndDuplicateSlots(t *testing.T) {
	raw := `{"version":"noopolis.causal-event.v1","run_id":"run-a","event_id":"moltnet:origin","emitter":{"system":"moltnet","stream_id":"s","seq":1},"type":"message.accepted","principal_id":"agent:agent-1","recorded_at":"2026-07-09T00:00:00.000Z","cause_event_ids":[],"payload":{}}
{"version":"noopolis.causal-event.v1","run_id":"run-b","event_id":"simfile:foreign","emitter":{"system":"simfile","stream_id":"s","seq":1},"type":"message.accepted","principal_id":"agent:agent-1","recorded_at":"2026-07-09T00:00:00.001Z","cause_event_ids":["moltnet:origin"],"payload":{}}`
	result := ParseCausalBundle(raw)
	if len(result.Errors) != 1 {
		t.Fatalf("expected exactly one cross-run error, got %d", len(result.Errors))
	}

	raw = `{"version":"noopolis.causal-event.v1","run_id":"run-a","event_id":"moltnet:slot","emitter":{"system":"moltnet","stream_id":"s","seq":1},"type":"message.accepted","principal_id":"agent:agent-1","recorded_at":"2026-07-09T00:00:00.000Z","cause_event_ids":[],"payload":{}}
{"version":"noopolis.causal-event.v1","run_id":"run-a","event_id":"moltnet:slot-2","emitter":{"system":"moltnet","stream_id":"s","seq":1},"type":"message.accepted","principal_id":"agent:agent-1","recorded_at":"2026-07-09T00:00:01.000Z","cause_event_ids":[],"payload":{}}`
	result = ParseCausalBundle(raw)
	if len(result.Errors) != 1 {
		t.Fatalf("expected duplicate event slot rejection, got %d", len(result.Errors))
	}
}
