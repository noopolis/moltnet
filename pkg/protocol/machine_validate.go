package protocol

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func (request MachineRequest) Validate() error {
	if request.Version != MachineProtocolV1 {
		return fmt.Errorf("version must be %q", MachineProtocolV1)
	}
	if err := validateBoundedIdentifier(request.CorrelationID, "correlation_id", MachineMaxCorrelationBytes); err != nil {
		return err
	}
	if err := validateMachineOperation(request.Operation); err != nil {
		return err
	}

	switch request.Operation {
	case MachineOpSendNudge:
		if request.SendNudge == nil || request.Read != nil || request.Subscribe != nil || request.Export != nil || request.Cancel != nil {
			return fmt.Errorf("send_nudge requires only send_nudge payload")
		}
		return request.SendNudge.Validate()
	case MachineOpRead:
		if request.Read == nil || request.SendNudge != nil || request.Subscribe != nil || request.Export != nil || request.Cancel != nil {
			return fmt.Errorf("read requires only read payload")
		}
		return request.Read.Validate()
	case MachineOpSubscribe:
		if request.Subscribe == nil || request.SendNudge != nil || request.Read != nil || request.Export != nil || request.Cancel != nil {
			return fmt.Errorf("subscribe requires only subscribe payload")
		}
		return request.Subscribe.Validate()
	case MachineOpExport:
		if request.Export == nil || request.SendNudge != nil || request.Read != nil || request.Subscribe != nil || request.Cancel != nil {
			return fmt.Errorf("export requires only export payload")
		}
		return request.Export.Validate()
	case MachineOpCancel:
		if request.Cancel == nil || request.SendNudge != nil || request.Read != nil || request.Subscribe != nil || request.Export != nil {
			return fmt.Errorf("cancel requires only cancel payload")
		}
		return request.Cancel.Validate()
	default:
		return fmt.Errorf("unsupported operation %q", request.Operation)
	}
}

func (response MachineResponse) Validate() error {
	if response.Version != MachineProtocolV1 {
		return fmt.Errorf("version must be %q", MachineProtocolV1)
	}
	if err := validateBoundedIdentifier(response.CorrelationID, "correlation_id", MachineMaxCorrelationBytes); err != nil {
		return err
	}
	if err := validateMachineOperation(response.Operation); err != nil {
		return err
	}

	payloadCount := 0
	if response.SendNudge != nil {
		payloadCount++
	}
	if response.Read != nil {
		payloadCount++
	}
	if response.Subscribe != nil {
		payloadCount++
	}
	if response.Export != nil {
		payloadCount++
	}
	if response.Cancel != nil {
		payloadCount++
	}
	if response.Event != nil {
		payloadCount++
	}
	if response.Error != nil {
		payloadCount++
	}
	if payloadCount != 1 {
		return fmt.Errorf("must include exactly one response payload, event, or error")
	}

	if response.Error != nil {
		if err := response.Error.Validate(); err != nil {
			return err
		}
	}

	switch response.Operation {
	case MachineOpSendNudge:
		if response.SendNudge != nil {
			return response.SendNudge.Validate()
		}
		if response.Event != nil {
			return fmt.Errorf("send_nudge does not support event payload")
		}
		return nil
	case MachineOpRead:
		if response.Read != nil {
			return response.Read.Validate()
		}
		if response.Event != nil {
			return fmt.Errorf("read does not support event payload")
		}
		return nil
	case MachineOpSubscribe:
		if response.Subscribe != nil {
			return response.Subscribe.Validate()
		}
		if response.Event != nil {
			return response.Event.Validate()
		}
		return nil
	case MachineOpExport:
		if response.Export != nil {
			return response.Export.Validate()
		}
		if response.Event != nil {
			return fmt.Errorf("export does not support event payload")
		}
		return nil
	case MachineOpCancel:
		if response.Cancel != nil {
			return response.Cancel.Validate()
		}
		if response.Event != nil {
			return fmt.Errorf("cancel does not support event payload")
		}
		return nil
	default:
		return fmt.Errorf("unsupported operation %q", response.Operation)
	}
}

func (request MachineSendNudgeRequest) Validate() error {
	if err := validateBoundedIdentifier(request.DeliveryID, "delivery_id", MachineMaxDeliveryBytes); err != nil {
		return err
	}
	if err := request.Target.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(request.Body) == "" {
		return fmt.Errorf("body is required")
	}
	if len(request.Body) > MachineMaxBodyBytes {
		return fmt.Errorf("body must be at most %d bytes", MachineMaxBodyBytes)
	}
	if strings.TrimSpace(request.OriginMessageID) != "" {
		if err := validateBoundedIdentifier(request.OriginMessageID, "origin_message_id", MachineMaxCursorBytes); err != nil {
			return err
		}
	}
	if len(request.CauseEventIDs) > 0 {
		if err := validateBoundedStringSlice(request.CauseEventIDs, MachineMaxCauseEventIDs, MachineMaxCauseBytes, "cause_event_ids"); err != nil {
			return err
		}
	}
	return nil
}

func (request MachineReadRequest) Validate() error {
	if err := request.Target.Validate(); err != nil {
		return err
	}
	if request.Limit < 1 || request.Limit > MachineMaxReadLimit {
		return fmt.Errorf("limit must be in range 1..%d", MachineMaxReadLimit)
	}
	if request.Before != "" && request.After != "" {
		return fmt.Errorf("before and after cannot both be set")
	}
	if request.Before != "" {
		if err := validateBoundedIdentifier(request.Before, "before", MachineMaxCursorBytes); err != nil {
			return err
		}
	}
	if request.After != "" {
		if err := validateBoundedIdentifier(request.After, "after", MachineMaxCursorBytes); err != nil {
			return err
		}
	}
	return nil
}

func (request MachineSubscribeRequest) Validate() error {
	if err := request.Target.Validate(); err != nil {
		return err
	}
	if request.MaxEvents < 1 || request.MaxEvents > MachineMaxSubscribeEvents {
		return fmt.Errorf("max_events must be in range 1..%d", MachineMaxSubscribeEvents)
	}
	if strings.TrimSpace(request.ResumeCursor) != "" {
		if err := validateBoundedIdentifier(request.ResumeCursor, "resume_cursor", MachineMaxCursorBytes); err != nil {
			return err
		}
	}
	return nil
}

func (request MachineExportRequest) Validate() error {
	if err := validateBoundedStringSlice(request.RoomIDs, MachineMaxExportRoomTargets, MachineMaxTargetBytes, "room_ids"); err != nil {
		return err
	}
	if err := validateBoundedStringSlice(request.DMPeerIDs, MachineMaxExportPeerTargets, MachineMaxTargetBytes, "dm_peer_ids"); err != nil {
		return err
	}
	if len(request.RoomIDs) == 0 && len(request.DMPeerIDs) == 0 {
		return fmt.Errorf("at least one of room_ids or dm_peer_ids is required")
	}
	return nil
}

func (request MachineCancelRequest) Validate() error {
	return validateBoundedIdentifier(request.TargetCorrelationID, "target_correlation_id", MachineMaxCorrelationBytes)
}

func (result MachineSendNudgeResult) Validate() error {
	if err := validateBoundedIdentifier(result.MessageID, "message_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if err := validateBoundedIdentifier(result.EventID, "event_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if result.ThreadID != "" {
		if err := validateBoundedIdentifier(result.ThreadID, "thread_id", MachineMaxTargetBytes); err != nil {
			return err
		}
	}
	if result.DMID != "" {
		if err := validateBoundedIdentifier(result.DMID, "dm_id", MachineMaxTargetBytes); err != nil {
			return err
		}
	}
	return nil
}

func (result MachineReadResult) Validate() error {
	return result.Target.Validate()
}

func (result MachineSubscribeResult) Validate() error {
	if result.Closed != MachineSubscribeClosed {
		return fmt.Errorf("closed must be %q", MachineSubscribeClosed)
	}
	switch result.Reason {
	case MachineSubscribeReasonDone, MachineSubscribeReasonLimit, MachineSubscribeReasonEOF:
	default:
		return fmt.Errorf("reason must be one of done, limit, or eof")
	}
	return nil
}

func (event MachineSubscribeEvent) Validate() error {
	if err := validateBoundedIdentifier(event.EventID, "event_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if strings.TrimSpace(event.Type) == "" {
		return fmt.Errorf("type is required")
	}
	if len(event.Payload) == 0 {
		return fmt.Errorf("payload is required")
	}
	if err := validateJSON(event.Payload); err != nil {
		return fmt.Errorf("payload %w", err)
	}
	return nil
}

func (result MachineExportResult) Validate() error {
	if result.Version != MachineExportSchemaVersion {
		return fmt.Errorf("version must be %q", MachineExportSchemaVersion)
	}
	if result.ControlMarker != MachineExportMarker {
		return fmt.Errorf("control_marker must be %q", MachineExportMarker)
	}
	if len(result.Transcript) > MachineMaxTranscriptBytes {
		return fmt.Errorf("transcript must be at most %d bytes", MachineMaxTranscriptBytes)
	}
	if len(result.TranscriptSHA) != 64 {
		return fmt.Errorf("transcript_sha256 must be 64 hex characters")
	}
	if _, err := hex.DecodeString(result.TranscriptSHA); err != nil {
		return fmt.Errorf("transcript_sha256 must be valid hex: %w", err)
	}
	return nil
}

func (result MachineCancelResult) Validate() error {
	if err := validateBoundedIdentifier(result.TargetCorrelationID, "target_correlation_id", MachineMaxCorrelationBytes); err != nil {
		return err
	}
	switch result.State {
	case MachineCancelStateCanceled, MachineCancelStateAlreadyFinal, MachineCancelStateNotFound:
	default:
		return fmt.Errorf("state must be canceled, already_final, or not_found")
	}
	return nil
}

func (target MachineTarget) Validate() error {
	switch target.Kind {
	case MachineTargetKindRoom, MachineTargetKindDM:
	default:
		return fmt.Errorf("kind must be room or dm")
	}
	return validateBoundedIdentifier(target.ID, "target.id", MachineMaxTargetBytes)
}

func (errorResponse MachineError) Validate() error {
	if strings.TrimSpace(errorResponse.Code) == "" {
		return fmt.Errorf("error code is required")
	}
	switch errorResponse.Code {
	case MachineErrorInvalidRequest, MachineErrorDuplicateRequest, MachineErrorUnsupported, MachineErrorNotFound,
		MachineErrorConflict, MachineErrorCapacity, MachineErrorTransport, MachineErrorCanceled:
	default:
		return fmt.Errorf("unknown error code %q", errorResponse.Code)
	}
	if strings.TrimSpace(errorResponse.Message) == "" {
		return fmt.Errorf("error message is required")
	}
	return nil
}

func validateMachineOperation(operation string) error {
	switch operation {
	case MachineOpSendNudge, MachineOpRead, MachineOpSubscribe, MachineOpExport, MachineOpCancel:
		return nil
	default:
		return fmt.Errorf("unsupported operation %q", operation)
	}
}

func validateBoundedIdentifier(value string, field string, max int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(trimmed) > max {
		return fmt.Errorf("%s must be at most %d bytes", field, max)
	}
	if err := ValidateMessageID(trimmed); err != nil {
		return fmt.Errorf("%s %w", field, err)
	}
	return nil
}

func validateBoundedStringSlice(values []string, maxCount int, maxLen int, field string) error {
	if len(values) == 0 {
		return nil
	}
	if len(values) > maxCount {
		return fmt.Errorf("%s must contain at most %d entries", field, maxCount)
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if err := validateBoundedIdentifier(value, fmt.Sprintf("%s[%d]", field, index), maxLen); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s must contain unique values", field)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateJSON(raw json.RawMessage) error {
	var marker any
	if err := json.Unmarshal(raw, &marker); err != nil {
		return err
	}
	return nil
}
