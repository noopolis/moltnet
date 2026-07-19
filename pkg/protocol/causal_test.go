package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validCausalEvent(t *testing.T) CausalEvent {
	t.Helper()
	payload, err := json.Marshal(MessageAcceptedPayload{
		MessageID:      "msg_1",
		Target:         Target{Kind: TargetKindRoom, RoomID: "general"},
		ContentSHA256:  "deadbeef",
		PolicyDecision: PolicyDecisionAccepted,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return CausalEvent{
		Version: CausalEventVersion,
		RunID:   "run_1",
		EventID: "moltnet:msg_1",
		Emitter: CausalEmitter{
			System:   CausalSystemMoltnet,
			StreamID: "network:local",
			Seq:      1,
		},
		Type:          EventTypeMessageAccepted,
		PrincipalID:   "agent:writer",
		RecordedAt:    time.Now().UTC(),
		CauseEventIDs: []string{},
		Payload:       payload,
	}
}

func TestCausalEventValidateAcceptsGoldenRecord(t *testing.T) {
	event := validCausalEvent(t)
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCausalEventValidateRejectsWrongVersion(t *testing.T) {
	event := validCausalEvent(t)
	event.Version = "noopolis.causal-event.v2"
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for wrong version")
	}
}

func TestCausalEventValidateRejectsMissingRunID(t *testing.T) {
	event := validCausalEvent(t)
	event.RunID = "  "
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for missing run_id")
	}
}

func TestCausalEventValidateRejectsMalformedEventID(t *testing.T) {
	for _, eventID := range []string{"", "no-colon-here", ":empty-system", "moltnet:"} {
		event := validCausalEvent(t)
		event.EventID = eventID
		if err := event.Validate(); err == nil {
			t.Fatalf("expected error for malformed event_id %q", eventID)
		}
	}
}

func TestCausalEventValidateRejectsEventIDSystemMismatch(t *testing.T) {
	event := validCausalEvent(t)
	event.EventID = "daimon:msg_1"
	if err := event.Validate(); err == nil {
		t.Fatal("expected error when event_id system prefix does not match emitter.system")
	}
}

func TestCausalEventValidateRejectsUnrecognizedSystem(t *testing.T) {
	event := validCausalEvent(t)
	event.Emitter.System = "spawnfile"
	event.EventID = "spawnfile:msg_1"
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for unrecognized emitter.system")
	}
}

func TestCausalEventValidateRejectsEmptyStreamID(t *testing.T) {
	event := validCausalEvent(t)
	event.Emitter.StreamID = "  "
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for empty stream_id")
	}
}

func TestCausalEventValidateRejectsNonPositiveSeq(t *testing.T) {
	for _, seq := range []int{0, -1} {
		event := validCausalEvent(t)
		event.Emitter.Seq = seq
		if err := event.Validate(); err == nil {
			t.Fatalf("expected error for seq %d", seq)
		}
	}
}

func TestCausalEventValidateRejectsMissingType(t *testing.T) {
	event := validCausalEvent(t)
	event.Type = ""
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestCausalEventValidateRejectsMissingPrincipalID(t *testing.T) {
	event := validCausalEvent(t)
	event.PrincipalID = ""
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for missing principal_id")
	}
}

func TestCausalEventValidateRejectsZeroRecordedAt(t *testing.T) {
	event := validCausalEvent(t)
	event.RecordedAt = time.Time{}
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for zero recorded_at")
	}
}

func TestCausalEventValidateRejectsNilCauseEventIDs(t *testing.T) {
	event := validCausalEvent(t)
	event.CauseEventIDs = nil
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for nil cause_event_ids")
	}
}

func TestCausalEventValidateRejectsBlankCauseEventID(t *testing.T) {
	event := validCausalEvent(t)
	event.CauseEventIDs = []string{"moltnet:msg_0", "  "}
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for blank cause_event_ids entry")
	}
}

func TestCausalEventValidateRejectsEmptyPayload(t *testing.T) {
	event := validCausalEvent(t)
	event.Payload = nil
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestCausalEventValidateRejectsNonObjectPayload(t *testing.T) {
	event := validCausalEvent(t)
	event.Payload = json.RawMessage(`"just a string"`)
	if err := event.Validate(); err == nil {
		t.Fatal("expected error for non-object payload")
	}
}

func TestCausalEventRoundTripsThroughJSON(t *testing.T) {
	event := validCausalEvent(t)
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CausalEvent
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("Validate() on round-tripped event error = %v", err)
	}

	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(reencoded) != string(encoded) {
		t.Fatalf("round trip mismatch:\n got  %s\n want %s", reencoded, encoded)
	}
}

func TestSendMessageRequestCauseEventIDsIsAdditive(t *testing.T) {
	request := SendMessageRequest{
		ID:            "msg_1",
		Target:        Target{Kind: TargetKindRoom, RoomID: "general"},
		From:          Actor{Type: "agent", ID: "writer"},
		Parts:         []Part{{Kind: PartKindText, Text: "hi"}},
		CauseEventIDs: []string{"daimon:turn_1"},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SendMessageRequest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.CauseEventIDs) != 1 || decoded.CauseEventIDs[0] != "daimon:turn_1" {
		t.Fatalf("expected cause_event_ids to round trip, got %#v", decoded.CauseEventIDs)
	}

	omitted := SendMessageRequest{
		ID:     "msg_2",
		Target: Target{Kind: TargetKindRoom, RoomID: "general"},
		From:   Actor{Type: "agent", ID: "writer"},
		Parts:  []Part{{Kind: PartKindText, Text: "hi"}},
	}
	encodedOmitted, err := json.Marshal(omitted)
	if err != nil {
		t.Fatalf("marshal omitted: %v", err)
	}
	if strings.Contains(string(encodedOmitted), "cause_event_ids") {
		t.Fatalf("expected cause_event_ids to be omitted when empty, got %s", encodedOmitted)
	}
}
