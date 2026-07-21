package protocol

import "testing"

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
		t.Fatalf("expected valid send_nudge request, got %v", err)
	}

	request.Operation = "bad"
	if err := request.Validate(); err == nil {
		t.Fatal("expected invalid operation rejection")
	}

	request.Operation = MachineOpRead
	request.Read = &MachineReadRequest{Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"}, Limit: 0}
	if err := request.Validate(); err == nil {
		t.Fatal("expected read limit rejection")
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

	if err := (MachineSendNudgeRequest{
		DeliveryID: "delivery_1",
		Target:     MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
		Body:       "wake body",
		CauseEventIDs: []string{
			"ev_1",
			"ev_1",
		},
	}).Validate(); err == nil {
		t.Fatal("expected duplicate cause rejection")
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
		t.Fatal("expected invalid limit")
	}
	if err := (MachineReadRequest{Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"}, Limit: 1, Before: "m1", After: "m2"}).Validate(); err == nil {
		t.Fatal("expected before/after exclusivity rejection")
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
}

func TestMachineExportValidation(t *testing.T) {
	t.Parallel()

	valid := MachineExportRequest{
		RoomIDs:       []string{"room_1", "room_2"},
		DMPeerIDs:     []string{"peer_1"},
		IncludeSocial: true,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid export payload: %v", err)
	}

	if err := (MachineExportRequest{}).Validate(); err == nil {
		t.Fatal("expected empty targets rejection")
	}

	if err := (MachineExportRequest{DMPeerIDs: make([]string, MachineMaxExportPeerTargets+1), IncludeSocial: true}).Validate(); err == nil {
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

	event := MachineSubscribeEvent{EventID: "ev_1", Type: "message", Payload: []byte(`{"a":1}`)}
	if err := event.Validate(); err != nil {
		t.Fatalf("expected valid subscribe event: %v", err)
	}

	exportResult := MachineExportResult{
		Version:       MachineExportSchemaVersion,
		ControlMarker: MachineExportMarker,
		Transcript:    "hello",
		TranscriptSHA: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	if err := exportResult.Validate(); err != nil {
		t.Fatalf("expected valid export result: %v", err)
	}

	if err := (MachineExportResult{Version: MachineExportSchemaVersion, ControlMarker: MachineExportMarker, Transcript: "hello", TranscriptSHA: "bad"}).Validate(); err == nil {
		t.Fatal("expected bad hash rejection")
	}
}
