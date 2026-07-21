package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		return MachineRequest{}, errors.New("empty request")
	}
	if len(raw) > MachineMaxInputLineBytes {
		return MachineRequest{}, fmt.Errorf("request exceeds %d bytes", MachineMaxInputLineBytes)
	}

	envelope, err := decodeJSONEnvelope(raw)
	if err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}
	request, err := parseMachineRequestEnvelope(envelope)
	if err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}
	if err := request.Validate(); err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}
	return request, nil
}

func DecodeMachineResponseLine(raw string) (MachineResponse, error) {
	if raw == "" {
		return MachineResponse{}, errors.New("empty response")
	}
	if len(raw) > MachineMaxOutputLineBytes {
		return MachineResponse{}, fmt.Errorf("response exceeds %d bytes", MachineMaxOutputLineBytes)
	}

	envelope, err := decodeJSONEnvelope(raw)
	if err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}
	response, err := parseMachineResponseEnvelope(envelope)
	if err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}
	if err := response.Validate(); err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}
	return response, nil
}

func EncodeMachineRequestLine(request MachineRequest) (string, error) {
	if err := request.Validate(); err != nil {
		return "", errors.New("invalid request")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return "", errors.New("invalid request")
	}
	if err := ensureSingleJSONValue(raw); err != nil {
		return "", errors.New("invalid request")
	}
	if len(raw) > MachineMaxInputLineBytes {
		return "", fmt.Errorf("request exceeds %d bytes", MachineMaxInputLineBytes)
	}
	return string(raw), nil
}

func EncodeMachineResponseLine(response MachineResponse) (string, error) {
	if err := response.Validate(); err != nil {
		return "", errors.New("invalid response")
	}
	raw, err := encodeMachineResponse(response)
	if err != nil {
		return "", errors.New("invalid response")
	}
	if err := ensureSingleJSONValue(raw); err != nil {
		return "", errors.New("invalid response")
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
			return MachineRequest{}, errors.New("invalid request")
		}
		payloadCount++
		payloadKey = key
	}
	if payloadCount != 1 {
		return MachineRequest{}, fmt.Errorf("exactly one payload is required")
	}

	var rawVersion, rawCorrelation, rawOperation json.RawMessage
	if err := requireField(envelope, "version", &rawVersion); err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}
	if err := requireField(envelope, "correlation_id", &rawCorrelation); err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}
	if err := requireField(envelope, "operation", &rawOperation); err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}

	if err := decodeStrictJSONValue(rawVersion, &request.Version); err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}
	if err := decodeStrictJSONValue(rawCorrelation, &request.CorrelationID); err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}
	if err := decodeStrictJSONValue(rawOperation, &request.Operation); err != nil {
		return MachineRequest{}, errors.New("invalid request")
	}

	if request.Operation != payloadKey {
		return MachineRequest{}, errors.New("operation mismatch")
	}

	payload, ok := machineRequestPayload(&request)
	if !ok {
		return MachineRequest{}, errors.New("invalid request")
	}
	if err := decodeStrictJSONValue(envelope[payloadKey], payload); err != nil {
		return MachineRequest{}, errors.New("invalid request")
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
			return MachineResponse{}, errors.New("invalid response")
		}
		payloadCount++
		payloadKey = key
	}
	if payloadCount != 1 {
		return MachineResponse{}, fmt.Errorf("exactly one of result payload, event, or error is required")
	}

	var rawVersion, rawCorrelation, rawOperation json.RawMessage
	if err := requireField(envelope, "version", &rawVersion); err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}
	if err := requireField(envelope, "correlation_id", &rawCorrelation); err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}
	if err := requireField(envelope, "operation", &rawOperation); err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}

	if err := decodeStrictJSONValue(rawVersion, &response.Version); err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}
	if err := decodeStrictJSONValue(rawCorrelation, &response.CorrelationID); err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}
	if err := decodeStrictJSONValue(rawOperation, &response.Operation); err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}

	if payloadKey == "error" {
		var errorPayload MachineError
		if err := decodeStrictJSONValue(envelope["error"], &errorPayload); err != nil {
			return MachineResponse{}, errors.New("invalid response")
		}
		response.Error = &errorPayload
		return response, nil
	}

	if response.Operation == MachineOpSubscribe && payloadKey == "event" {
		event, err := decodeMachineSubscribeEvent(envelope["event"])
		if err != nil {
			return MachineResponse{}, errors.New("invalid response")
		}
		response.Event = &event
		return response, nil
	}

	payload, ok := machineResponsePayload(&response)
	if !ok {
		return MachineResponse{}, errors.New("invalid response")
	}
	if payloadKey != response.Operation {
		return MachineResponse{}, errors.New("operation mismatch")
	}
	if err := decodeStrictJSONValue(envelope[payloadKey], payload); err != nil {
		return MachineResponse{}, errors.New("invalid response")
	}

	return response, nil
}

func machineRequestPayload(request *MachineRequest) (any, bool) {
	switch request.Operation {
	case MachineOpSendNudge:
		request.SendNudge = &MachineSendNudgeRequest{}
		return request.SendNudge, true
	case MachineOpRead:
		request.Read = &MachineReadRequest{}
		return request.Read, true
	case MachineOpSubscribe:
		request.Subscribe = &MachineSubscribeRequest{}
		return request.Subscribe, true
	case MachineOpExport:
		request.Export = &MachineExportRequest{}
		return request.Export, true
	case MachineOpCancel:
		request.Cancel = &MachineCancelRequest{}
		return request.Cancel, true
	default:
		return nil, false
	}
}

func machineResponsePayload(response *MachineResponse) (any, bool) {
	switch response.Operation {
	case MachineOpSendNudge:
		response.SendNudge = &MachineSendNudgeResult{}
		return response.SendNudge, true
	case MachineOpRead:
		response.Read = &MachineReadResult{}
		return response.Read, true
	case MachineOpSubscribe:
		response.Subscribe = &MachineSubscribeResult{}
		return response.Subscribe, true
	case MachineOpExport:
		response.Export = &MachineExportResult{}
		return response.Export, true
	case MachineOpCancel:
		response.Cancel = &MachineCancelResult{}
		return response.Cancel, true
	default:
		return nil, false
	}
}

func decodeMachineSubscribeEvent(raw json.RawMessage) (MachineSubscribeEvent, error) {
	decoded := MachineSubscribeEvent{}
	if isNull(raw) {
		return decoded, errors.New("invalid response")
	}

	parts, err := decodeJSONEnvelope(string(raw))
	if err != nil {
		return decoded, errors.New("invalid response")
	}
	if len(parts) != 3 {
		return decoded, errors.New("invalid response")
	}

	rawEventID, ok := parts["event_id"]
	if !ok {
		return decoded, errors.New("invalid response")
	}
	rawType, ok := parts["type"]
	if !ok {
		return decoded, errors.New("invalid response")
	}
	rawPayload, ok := parts["payload"]
	if !ok || isNull(rawPayload) {
		return decoded, errors.New("invalid response")
	}

	if err := decodeStrictJSONValue(rawEventID, &decoded.EventID); err != nil {
		return decoded, errors.New("invalid response")
	}
	if err := decodeStrictJSONValue(rawType, &decoded.Type); err != nil {
		return decoded, errors.New("invalid response")
	}
	decoded.Payload = rawPayload

	if err := decoded.Validate(); err != nil {
		return decoded, errors.New("invalid response")
	}
	return decoded, nil
}

func requireField(envelope map[string]json.RawMessage, field string, raw *json.RawMessage) error {
	value, ok := envelope[field]
	if !ok || isNull(value) {
		return errors.New("invalid request")
	}
	*raw = value
	return nil
}

func encodeMachineResponse(response MachineResponse) ([]byte, error) {
	if response.Error != nil {
		return json.Marshal(response)
	}

	switch response.Operation {
	case MachineOpSendNudge:
	case MachineOpRead:
	case MachineOpSubscribe:
		if response.Subscribe == nil {
			if response.Event == nil {
				return nil, errors.New("invalid response")
			}
			return encodeMachineResponseSubscribeEvent(response)
		}
	case MachineOpExport:
	case MachineOpCancel:
	case "":
		return nil, errors.New("invalid response")
	default:
		return nil, errors.New("invalid response")
	}

	return json.Marshal(response)
}

func encodeMachineResponseSubscribeEvent(response MachineResponse) ([]byte, error) {
	if response.Event == nil {
		return nil, errors.New("invalid response")
	}

	eventID, err := json.Marshal(response.Event.EventID)
	if err != nil {
		return nil, errors.New("invalid response")
	}
	eventType, err := json.Marshal(response.Event.Type)
	if err != nil {
		return nil, errors.New("invalid response")
	}

	var b strings.Builder
	b.WriteString(`{"version":`)
	if data, err := json.Marshal(response.Version); err == nil {
		b.Write(data)
	} else {
		return nil, errors.New("invalid response")
	}
	b.WriteString(`,"correlation_id":`)
	if data, err := json.Marshal(response.CorrelationID); err == nil {
		b.Write(data)
	} else {
		return nil, errors.New("invalid response")
	}
	b.WriteString(`,"operation":"subscribe","event":{"event_id":`)
	b.Write(eventID)
	b.WriteString(`,"type":`)
	b.Write(eventType)
	b.WriteString(`,"payload":`)
	b.Write(response.Event.Payload)
	b.WriteString(`}}`)
	return []byte(b.String()), nil
}
