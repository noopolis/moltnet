package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func boolPtr(value bool) *bool {
	return &value
}

func TestMachineProtocolConstantsArePositiveAndBounded(t *testing.T) {
	t.Parallel()

	if MachineMaxInputLineBytes <= 0 || MachineMaxOutputLineBytes <= 0 {
		t.Fatalf("line-byte bounds must be positive")
	}
	if MachineMaxCorrelationBytes <= 0 || MachineMaxDeliveryBytes <= 0 || MachineMaxTargetBytes <= 0 || MachineMaxBodyBytes <= 0 {
		t.Fatalf("identifier and body bounds must be positive")
	}
	if MachineMaxCursorBytes <= 0 || MachineMaxTranscriptBytes <= 0 {
		t.Fatalf("cursor/transcript bounds must be positive")
	}
	if MachineMaxExportRoomTargets <= 0 || MachineMaxExportPeerTargets <= 0 || MachineMaxReadLimit <= 0 || MachineMaxSubscribeEvents <= 0 {
		t.Fatalf("resource and behavior bounds must be positive")
	}
	if MachineMaxActiveRequests <= 0 || MachineMaxCorrelationRegistry <= 0 || MachineMaxDeliveryRegistry <= 0 {
		t.Fatalf("registry and active-request bounds must be positive")
	}
	if MachineMaxCauseBytes <= 0 || MachineMaxCauseEventIDs <= 0 {
		t.Fatalf("cause bounds must be positive")
	}
}

func TestMachineRequestValidationRejectsUnsupportedOperationAndBadShape(t *testing.T) {
	t.Parallel()

	request := MachineRequest{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_1",
		Operation:     MachineOpSendNudge,
		SendNudge: &MachineSendNudgeRequest{
			DeliveryID: "delivery_1",
			Target:     MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
			Body:       "wake body",
		},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	request.Operation = "bad"
	if err := request.Validate(); err == nil {
		t.Fatal("expected invalid operation rejection")
	}

	request.Operation = MachineOpRead
	request.Read = &MachineReadRequest{Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"}, Limit: 0}
	request.SendNudge = nil
	if err := request.Validate(); err == nil {
		t.Fatal("expected read limit rejection")
	}
}

func TestMachineIdentifierValidationRejectsWhitespaceAndScopedIDs(t *testing.T) {
	t.Parallel()

	request := MachineSubscribeRequest{
		Target:    MachineTarget{Kind: MachineTargetKindRoom, ID: " room_1"},
		MaxEvents: 1,
	}
	if err := request.Validate(); err == nil {
		t.Fatal("expected whitespace-bound identifier rejection")
	}

	request = MachineSubscribeRequest{
		Target:    MachineTarget{Kind: MachineTargetKindRoom, ID: "team/net_1"},
		MaxEvents: 1,
	}
	if err := request.Validate(); err == nil {
		t.Fatal("expected scoped id rejection")
	}
}

func TestMachineSendNudgeValidation(t *testing.T) {
	t.Parallel()

	valid := MachineSendNudgeRequest{
		DeliveryID: "delivery_1",
		Target:     MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
		Body:       "wake body",
		CauseEventIDs: []string{
			"ev_1",
			"ev_2",
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid send_nudge payload: %v", err)
	}

	if err := (MachineSendNudgeRequest{
		DeliveryID: "delivery_1",
		Target:     MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
		Body:       "",
	}).Validate(); err == nil {
		t.Fatal("expected empty body rejection")
	}

	overSizedBody := make([]byte, MachineMaxBodyBytes+1)
	if err := (MachineSendNudgeRequest{
		DeliveryID: "delivery_1",
		Target:     MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
		Body:       string(overSizedBody),
	}).Validate(); err == nil {
		t.Fatal("expected oversized body rejection")
	}
}

func TestMachineSendNudgeResultValidation(t *testing.T) {
	t.Parallel()

	valid := MachineSendNudgeResult{
		MessageID:     "msg_1",
		EventID:       "ev_1",
		Accepted:      boolPtr(true),
		ThreadCreated: boolPtr(true),
		ThreadID:      "thread_1",
		DMCreated:     boolPtr(false),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid result: %v", err)
	}

	if err := (MachineSendNudgeResult{MessageID: "msg_1", EventID: "ev_1"}).Validate(); err == nil {
		t.Fatal("expected omitted boolean fields rejection")
	}

	invalid := MachineSendNudgeResult{
		MessageID:     "msg_1",
		EventID:       "ev_1",
		Accepted:      boolPtr(true),
		ThreadCreated: boolPtr(false),
		ThreadID:      "thread_1",
		DMCreated:     boolPtr(false),
	}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected thread_id-only-when-false rejection")
	}
}

func TestMachineReadValidation(t *testing.T) {
	t.Parallel()

	if err := (MachineReadRequest{
		Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
		Limit:  MachineMaxReadLimit,
		After:  "m1",
	}).Validate(); err != nil {
		t.Fatalf("expected valid read payload: %v", err)
	}

	if err := (MachineReadRequest{Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"}, Limit: 0}).Validate(); err == nil {
		t.Fatal("expected invalid read limit")
	}
	if err := (MachineReadRequest{Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"}, Limit: 1, Before: "m1", After: "m2"}).Validate(); err == nil {
		t.Fatal("expected before/after exclusivity rejection")
	}
}

func TestMachineReadResultValidation(t *testing.T) {
	t.Parallel()

	validTarget := baseMachineReadMessage()

	result := MachineReadResult{
		Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
		Page: MachineReadPage{
			Messages: []MachineReadMessage{validTarget},
			Page: MachineReadPageInfo{
				HasMore:    boolPtr(true),
				NextAfter:  "msg_1",
				NextBefore: "msg_0",
			},
		},
	}
	if err := result.Validate(); err == nil {
		t.Fatal("expected dual cursor rejection")
	}

	basic := MachineReadResult{
		Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
		Page: MachineReadPage{
			Messages: []MachineReadMessage{validTarget},
			Page: MachineReadPageInfo{
				HasMore:   boolPtr(true),
				NextAfter: "msg_2",
			},
		},
	}
	if err := basic.Validate(); err != nil {
		t.Fatalf("expected valid read result: %v", err)
	}

	invalidTarget := baseMachineReadMessage()
	invalidTarget.Target.ThreadID = "thread_1"
	if err := (MachineReadResult{
		Target: basic.Target,
		Page: MachineReadPage{
			Messages: []MachineReadMessage{invalidTarget},
			Page:     basic.Page.Page,
		},
	}).Validate(); err == nil {
		t.Fatal("expected invalid room target shape")
	}

	oversizedID := baseMachineReadMessage()
	oversizedID.ID = strings.Repeat("x", MachineMaxTargetBytes+1)
	if err := (MachineReadResult{
		Target: basic.Target,
		Page: MachineReadPage{
			Messages: []MachineReadMessage{oversizedID},
			Page:     basic.Page.Page,
		},
	}).Validate(); err == nil {
		t.Fatal("expected oversized message id rejection")
	}

}

func TestMachineSubscribeValidation(t *testing.T) {
	t.Parallel()

	if err := (MachineSubscribeRequest{
		Target:    MachineTarget{Kind: MachineTargetKindDM, ID: "peer_1"},
		MaxEvents: 1,
	}).Validate(); err != nil {
		t.Fatalf("expected valid subscribe payload: %v", err)
	}

	if err := (MachineSubscribeRequest{
		Target:    MachineTarget{Kind: MachineTargetKindDM, ID: "peer_1"},
		MaxEvents: 0,
	}).Validate(); err == nil {
		t.Fatal("expected max_events lower bound rejection")
	}

	if err := (MachineSubscribeRequest{Target: MachineTarget{Kind: MachineTargetKindDM, ID: "peer_1"}, MaxEvents: MachineMaxSubscribeEvents + 1}).Validate(); err == nil {
		t.Fatal("expected max_events upper bound rejection")
	}

	event := MachineSubscribeEvent{
		EventID: "event_1",
		Type:    "message",
		Payload: json.RawMessage(`{"message":"ok"}`),
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("expected valid subscribe event: %v", err)
	}
	maxEventID := strings.Repeat("e", MachineMaxTargetBytes)
	maxType := strings.Repeat("t", MachineMaxTargetBytes)

	if err := (MachineSubscribeEvent{
		EventID: maxEventID,
		Type:    maxType,
		Payload: json.RawMessage(`{"value":"\"quoted\" and \\\\ backslash \\n in one payload"}`),
	}).Validate(); err != nil {
		t.Fatalf("expected escaped subscribe payload acceptance: %v", err)
	}
}

func TestMachineExportValidation(t *testing.T) {
	t.Parallel()

	includeSocial := true
	valid := MachineExportRequest{
		RoomIDs:       []string{"room_1", "room_2"},
		DMPeerIDs:     []string{"peer_1"},
		IncludeSocial: &includeSocial,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid export payload: %v", err)
	}

	if err := (MachineExportRequest{}).Validate(); err == nil {
		t.Fatal("expected empty targets rejection")
	}

	if err := (MachineExportRequest{
		DMPeerIDs:     make([]string, MachineMaxExportPeerTargets+1),
		IncludeSocial: &includeSocial,
	}).Validate(); err == nil {
		t.Fatal("expected export peer bound rejection")
	}

}

func TestMachineCancelValidation(t *testing.T) {
	t.Parallel()

	if err := (MachineCancelResult{TargetCorrelationID: "corr_1", State: MachineCancelStateCanceled}).Validate(); err != nil {
		t.Fatalf("expected valid cancel result: %v", err)
	}

	if err := (MachineCancelResult{TargetCorrelationID: "corr_1", State: "bad"}).Validate(); err == nil {
		t.Fatal("expected bad cancel state rejection")
	}
}

func TestMachineSubscribeEventAndExportResultValidation(t *testing.T) {
	t.Parallel()

	event := MachineSubscribeEvent{EventID: "ev_1", Type: "message", Payload: json.RawMessage(`{"a":1}`)}
	if err := event.Validate(); err != nil {
		t.Fatalf("expected valid subscribe event: %v", err)
	}

	hash := sha256sum("one\nline")
	exportResult := MachineExportResult{
		Version:       MachineExportSchemaVersion,
		ControlMarker: MachineExportMarker,
		Transcript:    "one\nline",
		TranscriptSHA: hash,
	}
	if err := exportResult.Validate(); err != nil {
		t.Fatalf("expected valid export result: %v", err)
	}

	if err := (MachineExportResult{
		Version:       MachineExportSchemaVersion,
		ControlMarker: MachineExportMarker,
		Transcript:    "one\nline",
		TranscriptSHA: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}).Validate(); err == nil {
		t.Fatal("expected bad hash rejection")
	}
}

func TestMachineRequestAndResponseErrorSentinelValidation(t *testing.T) {
	t.Parallel()

	if err := (MachineError{Code: MachineErrorInvalidRequest}).Validate(); err != nil {
		t.Fatalf("expected supported error code: %v", err)
	}

	if err := (MachineError{Code: "forbidden"}).Validate(); err == nil {
		t.Fatal("expected unsupported error code rejection")
	}

	secret := "attacker_code_987"
	response := MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_secret",
		Operation:     MachineOpRead,
		Error: &MachineError{
			Code: secret,
		},
	}
	if err := response.Validate(); err == nil {
		t.Fatal("expected secret response error code rejection")
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret response token: %v", err)
	}
}

func TestMachineErrorValidationRejectsInvalidCodes(t *testing.T) {
	t.Parallel()

	if err := (MachineError{Code: MachineErrorInvalidRequest}).Validate(); err != nil {
		t.Fatalf("expected valid error code: %v", err)
	}

	if (MachineError{}).Validate() == nil {
		t.Fatal("expected missing error code rejection")
	}

	if (MachineError{Code: "open"}).Validate() == nil {
		t.Fatal("expected unknown error code rejection")
	}
}

func baseMachineReadMessage() MachineReadMessage {
	return MachineReadMessage{
		ID:        "msg_1",
		NetworkID: "net_1",
		Origin:    MessageOrigin{NetworkID: "net_1", MessageID: "origin_1"},
		Target:    Target{Kind: TargetKindRoom, RoomID: "room_1"},
		From:      Actor{Type: "agent", ID: "agent_1"},
		Parts:     []Part{{Kind: PartKindText, Text: "hello"}},
		CreatedAt: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC),
	}
}

func sha256sum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
