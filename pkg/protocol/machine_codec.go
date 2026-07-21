package protocol

import (
	"encoding/json"
	"fmt"
)

var machineRequestPayloadKeys = map[string]struct{}{
	MachineOpSendNudge: {},
	MachineOpRead:      {},
	MachineOpSubscribe: {},
	MachineOpExport:    {},
	MachineOpCancel:    {},
}

var machineResponsePayloadKeys = map[string]struct{}{
	MachineOpSendNudge: {},
	MachineOpRead:      {},
	MachineOpSubscribe: {},
	MachineOpExport:    {},
	MachineOpCancel:    {},
	"event":            {},
	"error":            {},
}

func DecodeMachineRequestLine(raw string) (MachineRequest, error) {
	if raw == "" {
		return MachineRequest{}, fmt.Errorf("empty input")
	}
	if len(raw) > MachineMaxInputLineBytes {
		return MachineRequest{}, fmt.Errorf("request exceeds %d bytes", MachineMaxInputLineBytes)
	}

	envelope, err := decodeJSONEnvelope(raw)
	if err != nil {
		return MachineRequest{}, err
	}
	request, err := parseMachineRequestEnvelope(envelope)
	if err != nil {
		return MachineRequest{}, err
	}
	if err := request.Validate(); err != nil {
		return MachineRequest{}, err
	}
	return request, nil
}

func DecodeMachineResponseLine(raw string) (MachineResponse, error) {
	if raw == "" {
		return MachineResponse{}, fmt.Errorf("empty input")
	}
	if len(raw) > MachineMaxOutputLineBytes {
		return MachineResponse{}, fmt.Errorf("response exceeds %d bytes", MachineMaxOutputLineBytes)
	}

	envelope, err := decodeJSONEnvelope(raw)
	if err != nil {
		return MachineResponse{}, err
	}
	response, err := parseMachineResponseEnvelope(envelope)
	if err != nil {
		return MachineResponse{}, err
	}
	if err := response.Validate(); err != nil {
		return MachineResponse{}, err
	}
	return response, nil
}

func EncodeMachineRequestLine(request MachineRequest) (string, error) {
	if err := request.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	if err := ensureSingleJSONValue(raw); err != nil {
		return "", err
	}
	if len(raw) > MachineMaxInputLineBytes {
		return "", fmt.Errorf("request exceeds %d bytes", MachineMaxInputLineBytes)
	}
	return string(raw), nil
}

func EncodeMachineResponseLine(response MachineResponse) (string, error) {
	if err := response.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return "", err
	}
	if err := ensureSingleJSONValue(raw); err != nil {
		return "", err
	}
	if len(raw) > MachineMaxOutputLineBytes {
		return "", fmt.Errorf("response exceeds %d bytes", MachineMaxOutputLineBytes)
	}
	return string(raw), nil
}

func parseMachineRequestEnvelope(envelope map[string]json.RawMessage) (MachineRequest, error) {
	request := MachineRequest{}
	payloadKey := ""
	payloadCount := 0

	for key := range envelope {
		if key == "version" || key == "correlation_id" || key == "operation" {
			continue
		}
		if _, ok := machineRequestPayloadKeys[key]; !ok {
			return MachineRequest{}, fmt.Errorf("unknown field %q", key)
		}
		payloadCount++
		payloadKey = key
	}
	if payloadCount != 1 {
		return MachineRequest{}, fmt.Errorf("exactly one payload is required")
	}

	var rawVersion, rawCorrelation, rawOperation json.RawMessage
	if err := requireField(envelope, "version", &rawVersion); err != nil {
		return MachineRequest{}, err
	}
	if err := requireField(envelope, "correlation_id", &rawCorrelation); err != nil {
		return MachineRequest{}, err
	}
	if err := requireField(envelope, "operation", &rawOperation); err != nil {
		return MachineRequest{}, err
	}

	if err := decodeStrictJSONValue(rawVersion, &request.Version); err != nil {
		return MachineRequest{}, fmt.Errorf("version %w", err)
	}
	if err := decodeStrictJSONValue(rawCorrelation, &request.CorrelationID); err != nil {
		return MachineRequest{}, fmt.Errorf("correlation_id %w", err)
	}
	if err := decodeStrictJSONValue(rawOperation, &request.Operation); err != nil {
		return MachineRequest{}, fmt.Errorf("operation %w", err)
	}

	if request.Operation != payloadKey {
		return MachineRequest{}, fmt.Errorf("operation %q does not match payload %q", request.Operation, payloadKey)
	}

	switch request.Operation {
	case MachineOpSendNudge:
		var payload MachineSendNudgeRequest
		if err := decodeStrictJSONValue(envelope[MachineOpSendNudge], &payload); err != nil {
			return MachineRequest{}, fmt.Errorf("send_nudge %w", err)
		}
		request.SendNudge = &payload
	case MachineOpRead:
		var payload MachineReadRequest
		if err := decodeStrictJSONValue(envelope[MachineOpRead], &payload); err != nil {
			return MachineRequest{}, fmt.Errorf("read %w", err)
		}
		request.Read = &payload
	case MachineOpSubscribe:
		var payload MachineSubscribeRequest
		if err := decodeStrictJSONValue(envelope[MachineOpSubscribe], &payload); err != nil {
			return MachineRequest{}, fmt.Errorf("subscribe %w", err)
		}
		request.Subscribe = &payload
	case MachineOpExport:
		var payload MachineExportRequest
		if err := decodeStrictJSONValue(envelope[MachineOpExport], &payload); err != nil {
			return MachineRequest{}, fmt.Errorf("export %w", err)
		}
		request.Export = &payload
	case MachineOpCancel:
		var payload MachineCancelRequest
		if err := decodeStrictJSONValue(envelope[MachineOpCancel], &payload); err != nil {
			return MachineRequest{}, fmt.Errorf("cancel %w", err)
		}
		request.Cancel = &payload
	default:
		return MachineRequest{}, fmt.Errorf("unsupported operation %q", request.Operation)
	}

	return request, nil
}

func parseMachineResponseEnvelope(envelope map[string]json.RawMessage) (MachineResponse, error) {
	response := MachineResponse{}
	payloadKey := ""
	payloadCount := 0

	for key := range envelope {
		if key == "version" || key == "correlation_id" || key == "operation" {
			continue
		}
		if _, ok := machineResponsePayloadKeys[key]; !ok {
			return MachineResponse{}, fmt.Errorf("unknown field %q", key)
		}
		payloadCount++
		payloadKey = key
	}
	if payloadCount != 1 {
		return MachineResponse{}, fmt.Errorf("exactly one of result payload, event, or error is required")
	}

	var rawVersion, rawCorrelation, rawOperation json.RawMessage
	if err := requireField(envelope, "version", &rawVersion); err != nil {
		return MachineResponse{}, err
	}
	if err := requireField(envelope, "correlation_id", &rawCorrelation); err != nil {
		return MachineResponse{}, err
	}
	if err := requireField(envelope, "operation", &rawOperation); err != nil {
		return MachineResponse{}, err
	}

	if err := decodeStrictJSONValue(rawVersion, &response.Version); err != nil {
		return MachineResponse{}, fmt.Errorf("version %w", err)
	}
	if err := decodeStrictJSONValue(rawCorrelation, &response.CorrelationID); err != nil {
		return MachineResponse{}, fmt.Errorf("correlation_id %w", err)
	}
	if err := decodeStrictJSONValue(rawOperation, &response.Operation); err != nil {
		return MachineResponse{}, fmt.Errorf("operation %w", err)
	}

	if payloadKey == "error" {
		var errorPayload MachineError
		if err := decodeStrictJSONValue(envelope["error"], &errorPayload); err != nil {
			return MachineResponse{}, fmt.Errorf("error %w", err)
		}
		response.Error = &errorPayload
		return response, nil
	}

	switch response.Operation {
	case MachineOpSendNudge:
		if payloadKey != MachineOpSendNudge {
			return MachineResponse{}, fmt.Errorf("operation %q does not match payload %q", response.Operation, payloadKey)
		}
		var result MachineSendNudgeResult
		if err := decodeStrictJSONValue(envelope[MachineOpSendNudge], &result); err != nil {
			return MachineResponse{}, fmt.Errorf("send_nudge %w", err)
		}
		response.SendNudge = &result
	case MachineOpRead:
		if payloadKey != MachineOpRead {
			return MachineResponse{}, fmt.Errorf("operation %q does not match payload %q", response.Operation, payloadKey)
		}
		var result MachineReadResult
		if err := decodeStrictJSONValue(envelope[MachineOpRead], &result); err != nil {
			return MachineResponse{}, fmt.Errorf("read %w", err)
		}
		response.Read = &result
	case MachineOpSubscribe:
		if payloadKey == "event" {
			var event MachineSubscribeEvent
			if err := decodeStrictJSONValue(envelope["event"], &event); err != nil {
				return MachineResponse{}, fmt.Errorf("event %w", err)
			}
			response.Event = &event
			return response, nil
		}
		if payloadKey != MachineOpSubscribe {
			return MachineResponse{}, fmt.Errorf("operation %q does not match payload %q", response.Operation, payloadKey)
		}
		var result MachineSubscribeResult
		if err := decodeStrictJSONValue(envelope[MachineOpSubscribe], &result); err != nil {
			return MachineResponse{}, fmt.Errorf("subscribe %w", err)
		}
		response.Subscribe = &result
	case MachineOpExport:
		if payloadKey != MachineOpExport {
			return MachineResponse{}, fmt.Errorf("operation %q does not match payload %q", response.Operation, payloadKey)
		}
		var result MachineExportResult
		if err := decodeStrictJSONValue(envelope[MachineOpExport], &result); err != nil {
			return MachineResponse{}, fmt.Errorf("export %w", err)
		}
		response.Export = &result
	case MachineOpCancel:
		if payloadKey != MachineOpCancel {
			return MachineResponse{}, fmt.Errorf("operation %q does not match payload %q", response.Operation, payloadKey)
		}
		var result MachineCancelResult
		if err := decodeStrictJSONValue(envelope[MachineOpCancel], &result); err != nil {
			return MachineResponse{}, fmt.Errorf("cancel %w", err)
		}
		response.Cancel = &result
	default:
		return MachineResponse{}, fmt.Errorf("unsupported operation %q", response.Operation)
	}

	return response, nil
}

func requireField(envelope map[string]json.RawMessage, field string, raw *json.RawMessage) error {
	value, ok := envelope[field]
	if !ok || isNull(value) {
		return fmt.Errorf("missing field %q", field)
	}
	*raw = value
	return nil
}
