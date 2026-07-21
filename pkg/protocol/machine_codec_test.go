package protocol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestMachineCodecDecodeRejectsPhysicalLimitBypassAndWhitespaceOnly(t *testing.T) {
	t.Parallel()

	valid := `{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`
	padded := strings.Repeat(" ", MachineMaxInputLineBytes-len(valid)+1) + valid
	if _, err := DecodeMachineRequestLine(padded); err == nil {
		t.Fatal("expected oversize physical line rejection despite trim-valid payload")
	}

	if _, err := DecodeMachineRequestLine("   "); err == nil {
		t.Fatal("expected whitespace-only rejection")
	}
}

func TestMachineCodecRejectsDuplicateKeysTopLevelAndNested(t *testing.T) {
	t.Parallel()

	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`); err == nil {
		t.Fatal("expected top-level duplicate key rejection")
	}
	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"read","read":{"target":{"kind":"room","kind":"room","id":"room_1"},"limit":1}}`); err == nil {
		t.Fatal("expected nested duplicate key rejection")
	}
}

func TestMachineCodecRejectsMalformedAndPayloadMismatch(t *testing.T) {
	t.Parallel()

	if _, err := DecodeMachineRequestLine(`{"version":"bad","correlation_id":"corr_1","operation":"send_nudge","send_nudge":{"delivery_id":"del_1","target":{"kind":"room","id":"room_1"},"body":"x"}}`); err == nil {
		t.Fatal("expected version rejection")
	}
	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"send_nudge","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`); err == nil {
		t.Fatal("expected payload mismatch rejection")
	}
	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"send_nudge","send_nudge":{"delivery_id":"del_1","target":{"kind":"room","id":"room_1"},"body":"x"},"read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`); err == nil {
		t.Fatal("expected extra payload rejection")
	}
	if _, err := DecodeMachineResponseLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"read","send_nudge":{"message_id":"m1","event_id":"e1","accepted":true,"thread_created":false,"dm_created":false}}`); err == nil {
		t.Fatal("expected response payload mismatch rejection")
	}
}

func TestMachineCodecRejectsUnknownFieldsAndTypeDrift(t *testing.T) {
	t.Parallel()

	if _, err := DecodeMachineRequestLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"send_nudge","send_nudge":{"delivery_id":"del_1","target":{"kind":"room","id":"room_1"},"body":"x","forbidden_nested":["a"]}}`); err == nil {
		t.Fatal("expected nested unknown field rejection")
	}
	if _, err := DecodeMachineResponseLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"subscribe","subscribe":{"closed":"closed","reason":"done","unexpected":"x"}}`); err == nil {
		t.Fatal("expected extra field rejection in subscribe result")
	}
}

func TestMachineCodecErrorFramesAreCodeOnlyAndSecretSafe(t *testing.T) {
	t.Parallel()

	secret := `{"operation":"read","correlation_id":"corr_secret","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`
	response := MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_secret",
		Operation:     MachineOpRead,
		Error: &MachineError{
			Code: MachineErrorInvalidRequest,
		},
	}
	encoded, err := EncodeMachineResponseLine(response)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(encoded, "\"message\"") {
		t.Fatal("error frame must not include message")
	}
	if strings.Contains(encoded, secret) {
		t.Fatal("error frame must not echo encoded request payload bytes")
	}

	raw := `{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"read","error":{"code":"invalid_request","message":"` + secret + `"}}`
	if _, err := DecodeMachineResponseLine(raw); err == nil {
		t.Fatal("expected error payload unknown field rejection")
	}
}

func TestMachineCodecPreservesOpaqueSubscribePayloadBytes(t *testing.T) {
	t.Parallel()

	rawPayload := json.RawMessage(`{"payload":{"token":"api_key_123","value":{"a":1}},"meta":["x"]}`)

	response := MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_2",
		Operation:     MachineOpSubscribe,
		Event: &MachineSubscribeEvent{
			EventID: "evt_1",
			Type:    "message",
			Payload: rawPayload,
		},
	}
	responseLine, err := EncodeMachineResponseLine(response)
	if err != nil {
		t.Fatalf("response encode: %v", err)
	}
	decoded, err := DecodeMachineResponseLine(responseLine)
	if err != nil {
		t.Fatalf("response decode: %v", err)
	}
	if !bytes.Equal(decoded.Event.Payload, rawPayload) {
		t.Fatal("provider payload bytes changed by validation/encoding")
	}
}

func TestMachineCodecEventPayloadAndSubscribeResultBounds(t *testing.T) {
	t.Parallel()

	invalid := MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_1",
		Operation:     MachineOpSubscribe,
		Event: &MachineSubscribeEvent{
			EventID: "e1",
			Type:    strings.Repeat("x", MachineMaxTargetBytes+1),
			Payload: json.RawMessage(`{"message":"hi"}`),
		},
	}
	if _, err := EncodeMachineResponseLine(invalid); err == nil {
		t.Fatal("expected invalid event type bound rejection")
	}

	raw := `{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"subscribe","event":{"event_id":"e1","type":"message","payload":` + strings.Repeat("{", 2) + `}}`
	if _, err := DecodeMachineResponseLine(raw); err == nil {
		t.Fatal("expected malformed event payload rejection")
	}
}

func TestMachineCodecDecodeEncodeRoundTrip(t *testing.T) {
	t.Parallel()

	response := MachineResponse{
		Version:       MachineProtocolV1,
		CorrelationID: "corr_sub_1",
		Operation:     MachineOpSubscribe,
		Event: &MachineSubscribeEvent{
			EventID: "event_1",
			Type:    "message",
			Payload: json.RawMessage(`{"message":"m"}`),
		},
	}
	encoded, err := EncodeMachineResponseLine(response)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeMachineResponseLine(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Operation != MachineOpSubscribe || decoded.Event == nil || decoded.Event.Type != "message" {
		t.Fatalf("unexpected decoded subscribe response: %#v", decoded)
	}

	hexEncoded := hex.EncodeToString([]byte(encoded))
	if len(hexEncoded) == 0 {
		t.Fatal("round-trip produced empty encoding")
	}
}
