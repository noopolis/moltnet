package protocol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	tests := []struct {
		name        string
		correlation string
		payload     json.RawMessage
	}{
		{
			name:        "pretty payload preserved",
			correlation: "corr_2",
			payload: json.RawMessage(`{
  "payload": {
    "token":"api_key_123",
    "value":{"a":1}
  },
  "meta":["x"]
}`),
		},
		{
			name:        "secret payload raw bytes preserved",
			correlation: "corr_sub_payload",
			payload:     json.RawMessage("{\n  \"token\": \"value_with_secret_xyz\",\n  \"payload\": {\n    \"deep\": {\n      \"line\": \"keep\"\n    }\n  }\n}"),
		},
	}
	for _, tc := range tests {
		response := MachineResponse{
			Version:       MachineProtocolV1,
			CorrelationID: tc.correlation,
			Operation:     MachineOpSubscribe,
			Event: &MachineSubscribeEvent{
				EventID: "evt_1",
				Type:    "message",
				Payload: tc.payload,
			},
		}
		encoded, err := EncodeMachineResponseLine(response)
		if err != nil {
			t.Fatalf("%s: response encode: %v", tc.name, err)
		}
		decoded, err := DecodeMachineResponseLine(encoded)
		if err != nil {
			t.Fatalf("%s: response decode: %v", tc.name, err)
		}
		if !bytes.Equal(decoded.Event.Payload, tc.payload) {
			t.Fatalf("%s: provider payload bytes changed by validation/encoding", tc.name)
		}
	}

	if _, err := DecodeMachineResponseLine(`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"subscribe","event":{"event_id":"evt","type":"message","payload":{"token":"a",},"payload":""}}`); err == nil {
		t.Fatal("expected malformed payload rejection")
	}

	duplicatePayload := `{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_1","operation":"subscribe","event":{"event_id":"evt","type":"message","payload":{"token":"a","token":"b"}}}`
	if _, err := DecodeMachineResponseLine(duplicatePayload); err == nil {
		t.Fatal("expected duplicate payload rejection")
	}
}

func TestMachineCodecSentinelProofSafeErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw    string
		secret string
	}{
		{`{"version":"` + MachineProtocolV1 + `","version":"v","correlation_id":"secret_request_42","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"limit":1}}`, "secret_request_42"},
		{`{"version":"` + MachineProtocolV1 + `","correlation_id":"secret_response_99","operation":"send_nudge","read":{"message_id":"m1","event_id":"e1","accepted":true,"thread_created":true,"thread_id":"t1","dm_created":false}}`, "secret_response_99"},
		{`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_err_unknown","operation":"send_nudge","error":{"code":"open sesame"}}`, "open sesame"},
		{`{"version":"` + MachineProtocolV1 + `","correlation_id":"corr_dup_1","operation":"read","read":{"target":{"kind":"room","id":"room_1","id":"room_2"},"limit":1}}`, "room_2"},
		{`{"version":"` + MachineProtocolV1 + `","correlation_id":"bad_json_secret","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"limit":1`, "bad_json_secret"},
	} {
		assertMachineResponseDecodeErrorSafe(t, tc.raw, tc.secret, "open sesame")
	}

	for _, tc := range []struct {
		name  string
		build func() MachineResponse
	}{{name: "invalid mismatch payload", build: func() MachineResponse {
		return MachineResponse{
			Version:       MachineProtocolV1,
			CorrelationID: "corr_send_err",
			Operation:     MachineOpRead,
			SendNudge: &MachineSendNudgeResult{
				MessageID:     "m1",
				EventID:       "e1",
				Accepted:      boolPtr(true),
				ThreadCreated: boolPtr(true),
				ThreadID:      "t1",
				DMCreated:     boolPtr(true),
			},
		}
	}},
		{name: "unknown error code boundary", build: func() MachineResponse {
			return MachineResponse{
				Version:       MachineProtocolV1,
				CorrelationID: "corr_err_boundary",
				Operation:     MachineOpSendNudge,
				Error: &MachineError{
					Code: "open sesame",
				},
			}
		}},
	} {
		response := tc.build()
		if err := response.Validate(); err == nil {
			t.Fatalf("%s: expected boundary validation failure", tc.name)
		} else {
			assertMachineErrorSafeText(t, err, "open sesame")
		}
		if _, err := EncodeMachineResponseLine(response); err == nil {
			t.Fatalf("%s: expected encode failure", tc.name)
		}
	}
}

func assertMachineResponseDecodeErrorSafe(t *testing.T, raw, secret, sentinel string) {
	t.Helper()
	_, err := DecodeMachineResponseLine(raw)
	if err == nil {
		t.Fatal("expected decode failure for case")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("decode error leaked hostile value %q", secret)
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatal("decode error leaked sentinel token")
	}
}

func assertMachineErrorSafeText(t *testing.T, err error, sentinel string) {
	t.Helper()
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("error text leaked sentinel token")
	}
}

func TestMachineCodecReadHostileNestedPageValidation(t *testing.T) {
	t.Parallel()

	baseMessage := `{"id":"m1","network_id":"net_1","origin":{"network_id":"net_1","message_id":"origin_1"},"target":{"kind":"room","room_id":"room_1"},"from":{"type":"agent","id":"agent_1"},"parts":[{"kind":"text","text":"hello"}],"created_at":"2026-07-21T00:00:00Z"}`
	valid := fmt.Sprintf(`{"version":"%s","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[%s],"page":{"has_more":false}}}}`, MachineProtocolV1, baseMessage)
	if _, err := DecodeMachineResponseLine(valid); err != nil {
		t.Fatalf("expected valid baseline read response: %v", err)
	}

	tests := []struct {
		name      string
		raw       string
		forbidden string
	}{
		{
			name:      "missing message identity",
			raw:       fmt.Sprintf(`{"version":"%s","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[{"network_id":"net_1","origin":{"network_id":"net_1","message_id":"origin_1"},"target":{"kind":"room","room_id":"room_1"},"from":{"type":"agent","id":"agent_1"},"parts":[{"kind":"text","text":"hello"}],"created_at":"2026-07-21T00:00:00Z"}],"page":{"has_more":false}}}}`, MachineProtocolV1),
			forbidden: "",
		},
		{
			name:      "invalid origin id",
			raw:       fmt.Sprintf(`{"version":"%s","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[{"id":"m1","network_id":"net_1","origin":{"network_id":"net_1","message_id":"bad msg"},"target":{"kind":"room","room_id":"room_1"},"from":{"type":"agent","id":"agent_1"},"parts":[{"kind":"text","text":"hello"}],"created_at":"2026-07-21T00:00:00Z"}],"page":{"has_more":false}}}}`, MachineProtocolV1),
			forbidden: "bad msg",
		},
		{
			name:      "irrelevant target fields",
			raw:       fmt.Sprintf(`{"version":"%s","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[{"id":"m1","network_id":"net_1","origin":{"network_id":"net_1","message_id":"origin_1"},"target":{"kind":"room","room_id":"room_1","thread_id":"th1"},"from":{"type":"agent","id":"agent_1"},"parts":[{"kind":"text","text":"hello"}],"created_at":"2026-07-21T00:00:00Z"}],"page":{"has_more":false}}}}`, MachineProtocolV1),
			forbidden: "",
		},
		{
			name:      "invalid nested URL",
			raw:       fmt.Sprintf(`{"version":"%s","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[{"id":"m1","network_id":"net_1","origin":{"network_id":"net_1","message_id":"origin_1"},"target":{"kind":"room","room_id":"room_1"},"from":{"type":"agent","id":"agent_1"},"parts":[{"kind":"url","url":"ftp://bad"}],"created_at":"2026-07-21T00:00:00Z"}],"page":{"has_more":false}}}}`, MachineProtocolV1),
			forbidden: "ftp://bad",
		},
		{
			name:      "invalid time format",
			raw:       fmt.Sprintf(`{"version":"%s","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[{"id":"m1","network_id":"net_1","origin":{"network_id":"net_1","message_id":"origin_1"},"target":{"kind":"room","room_id":"room_1"},"from":{"type":"agent","id":"agent_1"},"parts":[{"kind":"text","text":"hello"}],"created_at":"21/07/2026"}],"page":{"has_more":false}}}}`, MachineProtocolV1),
			forbidden: "21/07/2026",
		},
	}

	for _, tc := range tests {
		_, err := DecodeMachineResponseLine(tc.raw)
		if err == nil {
			t.Fatalf("%s: expected hostile nested payload rejection", tc.name)
		}
		if tc.forbidden != "" && strings.Contains(err.Error(), tc.forbidden) {
			t.Fatalf("%s: leaked hostile token in error", tc.name)
		}
	}

	parts := make([]string, MachineMaxReadMessageParts+1)
	for i := range parts {
		parts[i] = `{"kind":"text","text":"x"}`
	}
	raw := fmt.Sprintf(`{"version":"%s","correlation_id":"corr_read_parts","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[{"id":"m1","network_id":"net_1","origin":{"network_id":"net_1","message_id":"origin_1"},"target":{"kind":"room","room_id":"room_1"},"from":{"type":"agent","id":"agent_1"},"parts":[%s],"created_at":"2026-07-21T00:00:00Z"}],"page":{"has_more":false}}}}`, MachineProtocolV1, strings.Join(parts, ","))
	if _, err := DecodeMachineResponseLine(raw); err == nil {
		t.Fatal("expected oversized message part rejection")
	}
}

func TestMachineCodecAcceptsNilReadMessagesAsDeclared(t *testing.T) {
	t.Parallel()
	hasMore := false
	response := MachineResponse{
		Version: MachineProtocolV1, CorrelationID: "corr_nil_messages", Operation: MachineOpRead,
		Read: &MachineReadResult{Target: MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"}, Page: MachineReadPage{
			Page: MachineReadPageInfo{HasMore: &hasMore},
		}},
	}
	line, err := EncodeMachineResponseLine(response)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeMachineResponseLine(line)
	if err != nil || decoded.Read == nil {
		t.Fatalf("round trip: %#v / %v", decoded, err)
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
