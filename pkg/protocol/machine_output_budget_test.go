package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMachineSubscribeEventOutputEnvelopeBudgetProof(t *testing.T) {
	t.Parallel()

	maxEventID := strings.Repeat("e", MachineMaxTargetBytes)
	maxType := strings.Repeat("t", MachineMaxTargetBytes)
	low, high := 0, MachineMaxOutputLineBytes
	for low < high {
		mid := (low + high + 1) / 2
		event := MachineSubscribeEvent{
			EventID: maxEventID,
			Type:    maxType,
			Payload: json.RawMessage(`{"value":"` + strings.Repeat("a", mid) + `"}`),
		}
		if event.Validate() == nil {
			low = mid
		} else {
			high = mid - 1
		}
	}
	nearBoundary := json.RawMessage(`{"value":"` + strings.Repeat("a", low) + `"}`)
	maxBoundaryEvent := MachineSubscribeEvent{
		EventID: maxEventID,
		Type:    maxType,
		Payload: nearBoundary,
	}
	if err := maxBoundaryEvent.Validate(); err != nil {
		t.Fatalf("expected near-boundary subscribe event: %v", err)
	}
	if _, err := EncodeMachineResponseLine(MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: strings.Repeat("x", MachineMaxCorrelationBytes),
		Operation:     MachineOpSubscribe,
		Event:         &maxBoundaryEvent,
	}); err != nil {
		t.Fatalf("expected near-boundary subscribe encode: %v", err)
	}
	if err := (MachineSubscribeEvent{
		EventID: maxEventID,
		Type:    maxType,
		Payload: json.RawMessage(`{"value":"` + strings.Repeat("a", low+1) + `"}`),
	}).Validate(); err == nil {
		t.Fatal("expected oversized subscribe payload rejection")
	}
}

func TestMachineExportResultOutputEnvelopeBudgetProof(t *testing.T) {
	t.Parallel()

	low, high := 0, MachineMaxTranscriptBytes
	for low < high {
		mid := (low + high + 1) / 2
		transcript := strings.Repeat("a", mid)
		result := MachineExportResult{
			Version:       MachineExportSchemaVersion,
			ControlMarker: MachineExportMarker,
			Transcript:    transcript,
			TranscriptSHA: sha256sum(transcript),
		}
		if result.Validate() == nil {
			low = mid
		} else {
			high = mid - 1
		}
	}
	nearBoundary := strings.Repeat("a", low)
	result := MachineExportResult{
		Version:       MachineExportSchemaVersion,
		ControlMarker: MachineExportMarker,
		Transcript:    nearBoundary,
		TranscriptSHA: sha256sum(nearBoundary),
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("expected near-boundary export result: %v", err)
	}
	if _, err := EncodeMachineResponseLine(MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: strings.Repeat("x", MachineMaxCorrelationBytes),
		Operation:     MachineOpExport,
		Export:        &result,
	}); err != nil {
		t.Fatalf("expected near-boundary export encode: %v", err)
	}

	hostileLen := (MachineMaxTranscriptBytes - 1) / 2
	hostileTranscript := strings.Repeat(`"`, hostileLen) + "\n" + strings.Repeat(`\`, hostileLen)
	if err := (MachineExportResult{
		Version:       MachineExportSchemaVersion,
		ControlMarker: MachineExportMarker,
		Transcript:    hostileTranscript,
		TranscriptSHA: sha256sum(hostileTranscript),
	}).Validate(); err == nil {
		t.Fatal("expected escaped-heavy transcript rejection")
	}
}

func TestMachineReadMessageActorNameAndFQIDOutputBudgetProof(t *testing.T) {
	t.Parallel()

	bounded := baseMachineReadMessage()
	bounded.From.Name = strings.Repeat("x", MachineMaxTargetBytes)
	bounded.From.FQID = strings.Repeat("x", MachineMaxTargetBytes)
	if err := machineOutputBudgetReadResult(bounded).Validate(); err != nil {
		t.Fatalf("expected bounded actor name/fqid to pass: %v", err)
	}

	overSizedActorName := baseMachineReadMessage()
	overSizedActorName.From.Name = strings.Repeat("x", MachineMaxTargetBytes+1)
	if err := machineOutputBudgetReadResult(overSizedActorName).Validate(); err == nil {
		t.Fatal("expected oversized actor name rejection")
	}

	overSizedActorFQID := baseMachineReadMessage()
	overSizedActorFQID.From.FQID = strings.Repeat("x", MachineMaxTargetBytes+1)
	if err := machineOutputBudgetReadResult(overSizedActorFQID).Validate(); err == nil {
		t.Fatal("expected oversized actor fqid rejection")
	}
}

func TestMachineReadPageValidationRespectsEncodedResponseLimit(t *testing.T) {
	t.Parallel()

	partText := strings.Repeat("x", 500)
	maxMessages := maxReadMessagesForPartText(partText)
	nearBoundary := machineReadPageWithText(maxMessages, partText)
	if err := nearBoundary.Validate(); err != nil {
		t.Fatalf("expected near-boundary read page: %v", err)
	}

	if _, err := EncodeMachineResponseLine(MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: strings.Repeat("x", MachineMaxCorrelationBytes),
		Operation:     MachineOpRead,
		Read: &MachineReadResult{
			Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
			Page:   nearBoundary,
		},
	}); err != nil {
		t.Fatalf("expected near-boundary read encode: %v", err)
	}
	if maxMessages < MachineMaxReadLimit {
		overflow := machineReadPageWithText(maxMessages+1, partText)
		if err := overflow.Validate(); err == nil {
			t.Fatal("expected aggregated read-page overflow rejection")
		}
	}
}

func machineReadPageWithText(messageCount int, partText string) MachineReadPage {
	messages := make([]MachineReadMessage, messageCount)
	for i := range messages {
		message := baseMachineReadMessage()
		message.Parts = []Part{{Kind: PartKindText, Text: partText}}
		messages[i] = message
	}
	return MachineReadPage{
		Messages: messages,
		Page:     MachineReadPageInfo{HasMore: boolPtr(false)},
	}
}

func maxReadMessagesForPartText(partText string) int {
	low, high := 0, MachineMaxReadLimit
	for low < high {
		mid := (low + high + 1) / 2
		if machineReadPageWithText(mid, partText).Validate() == nil {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return low
}

func machineOutputBudgetReadResult(message MachineReadMessage) MachineReadResult {
	return MachineReadResult{Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"}, Page: MachineReadPage{Messages: []MachineReadMessage{message}, Page: MachineReadPageInfo{HasMore: boolPtr(false)}}}
}
