package protocol

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestMachineCodecDecodeRejectsEmptyMalformedMultipleAndOversized(t *testing.T) {
	t.Parallel()

	if _, err := DecodeMachineRequestLine(""); err == nil {
		t.Fatal("expected empty input rejection")
	}
	if _, err := DecodeMachineRequestLine("   "); err == nil {
		t.Fatal("expected whitespace-only input rejection")
	}
	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `"`); err == nil {
		t.Fatal("expected malformed input rejection")
	}

	oversizedCorrelation := strings.Repeat("a", MachineMaxInputLineBytes+1)
	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"` + oversizedCorrelation + `","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`); err == nil {
		t.Fatal("expected oversized input rejection")
	}

	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_2","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`); err == nil {
		t.Fatal("expected multiple JSON values rejection")
	}
}

func TestMachineCodecRejectsVersionMismatchPayloadMismatchAndUnsupportedFields(t *testing.T) {
	t.Parallel()

	if _, err := DecodeMachineRequestLine(`{"version":"bad","correlation_id":"corr_1","operation":"send_nudge","send_nudge":{"delivery_id":"del_1","target":{"kind":"room","id":"room_1"},"body":"x"}}`); err == nil {
		t.Fatal("expected version rejection")
	}
	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"send_nudge","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`); err == nil {
		t.Fatal("expected payload mismatch rejection")
	}
	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"send_nudge","send_nudge":{"delivery_id":"del_1","target":{"kind":"room","id":"room_1"},"body":"x"},"read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`); err == nil {
		t.Fatal("expected multi payload rejection")
	}
	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"send_nudge","send_nudge":{"delivery_id":"del_1","target":{"kind":"room","id":"room_1"},"body":"x","forbidden_nested":["a"]}}`); err == nil {
		t.Fatal("expected nested forbidden field rejection")
	}
}

func TestMachineCodecRejectsUnknownTopLevelField(t *testing.T) {
	t.Parallel()

	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"send_nudge","send_nudge":{"delivery_id":"del_1","target":{"kind":"room","id":"room_1"},"body":"x"},"forbidden":"bad"}`); err == nil {
		t.Fatal("expected top-level unknown field rejection")
	}
}

func TestMachineResponseCodecRejectsCombinationViolations(t *testing.T) {
	t.Parallel()

	if _, err := DecodeMachineResponseLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[],"page":{"has_more":false}},"error":{"code":"invalid_request","message":"bad"}}`); err == nil {
		t.Fatal("expected one-of violation rejection")
	}
	if _, err := DecodeMachineResponseLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"subscribe","send_nudge":{"message_id":"m1","event_id":"e1","accepted":true,"thread_created":false,"dm_created":false}}`); err == nil {
		t.Fatal("expected op/payload mismatch rejection")
	}
	if _, err := DecodeMachineResponseLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"subscribe","event":{"event_id":"e1","type":"message","payload":{"message":"hi"},"bad":"no"}}`); err == nil {
		t.Fatal("expected nested unknown event field rejection")
	}

	if _, err := DecodeMachineResponseLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"send_nudge","error":{"code":"unsupported","message":"no"}}`); err != nil {
		t.Fatalf("expected send_nudge unsupported error response decode: %v", err)
	}
}

func TestMachineCodecSecretSafetyInErrors(t *testing.T) {
	t.Parallel()

	request := MachineRequest{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_1",
		Operation:     MachineOpSendNudge,
		SendNudge: &MachineSendNudgeRequest{
			DeliveryID: "delivery_1",
			Target:     MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"},
			Body:       "top secret: token=abc",
		},
	}
	requestLine, err := EncodeMachineRequestLine(request)
	if err != nil {
		t.Fatalf("request encode: %v", err)
	}
	if !strings.Contains(requestLine, "token") {
		t.Fatal("request should include body content for fixture")
	}

	response := MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_1",
		Operation:     MachineOpSendNudge,
		Error: &MachineError{
			Code:    MachineErrorInvalidRequest,
			Message: "request rejected",
		},
	}
	responseLine, err := EncodeMachineResponseLine(response)
	if err != nil {
		t.Fatalf("response encode: %v", err)
	}
	if strings.Contains(responseLine, "token") {
		t.Fatal("error response must not echo private request body")
	}
	if strings.Contains(requestLine, "token") == false {
		t.Fatalf("request fixture missing token marker: %s", requestLine)
	}

	if strings.Contains(responseLine, "token") {
		t.Fatal("error response leak check failed")
	}

	_ = requestLine
}

func TestMachineResponseOutputBoundRejectsLongErrorMessage(t *testing.T) {
	t.Parallel()

	response := MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_long",
		Operation:     MachineOpSendNudge,
		Error: &MachineError{
			Code:    MachineErrorCapacity,
			Message: strings.Repeat("x", MachineMaxOutputLineBytes),
		},
	}
	if _, err := EncodeMachineResponseLine(response); err == nil {
		t.Fatal("expected response length rejection")
	}
}

func TestMachineCodecTransportRoundTripAndValidation(t *testing.T) {
	t.Parallel()

	subs := MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_sub_1",
		Operation:     MachineOpSubscribe,
		Event: &MachineSubscribeEvent{
			EventID: "event_1",
			Type:    "message",
			Payload: json.RawMessage(`{"x":1}`),
		},
	}
	encoded, err := EncodeMachineResponseLine(subs)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeMachineResponseLine(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Operation != MachineOpSubscribe || decoded.Event == nil {
		t.Fatalf("unexpected subscribe decode: %#v", decoded)
	}
	if decoded.Event.Type != "message" {
		t.Fatalf("unexpected event type: %s", decoded.Event.Type)
	}
	encodedHex := hex.EncodeToString([]byte(encoded))
	if len(encodedHex) == 0 {
		t.Fatal("round-trip produced empty encoding")
	}
}
