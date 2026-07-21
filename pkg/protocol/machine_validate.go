package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func (request MachineRequest) Validate() error {
	if request.Version != MachineProtocolV1 {
		return fmt.Errorf("version must be %q", MachineProtocolV1)
	}
	if err := validateMachineIdentifier(request.CorrelationID, "correlation_id", MachineMaxCorrelationBytes); err != nil {
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
	if err := validateMachineIdentifier(response.CorrelationID, "correlation_id", MachineMaxCorrelationBytes); err != nil {
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
		return response.Error.Validate()
	}

	switch response.Operation {
	case MachineOpSendNudge:
		if response.SendNudge == nil {
			return fmt.Errorf("send_nudge payload is required")
		}
		if response.Read != nil || response.Subscribe != nil || response.Export != nil || response.Cancel != nil || response.Event != nil {
			return fmt.Errorf("send_nudge response contains unsupported payloads")
		}
		return response.SendNudge.Validate()
	case MachineOpRead:
		if response.Read == nil {
			return fmt.Errorf("read payload is required")
		}
		if response.SendNudge != nil || response.Subscribe != nil || response.Export != nil || response.Cancel != nil || response.Event != nil {
			return fmt.Errorf("read response contains unsupported payloads")
		}
		return response.Read.Validate()
	case MachineOpSubscribe:
		if response.Read != nil || response.SendNudge != nil || response.Export != nil || response.Cancel != nil {
			return fmt.Errorf("subscribe response contains unsupported payloads")
		}
		if response.Subscribe == nil && response.Event == nil {
			return fmt.Errorf("subscribe payload is required")
		}
		if response.Subscribe != nil {
			return response.Subscribe.Validate()
		}
		return response.Event.Validate()
	case MachineOpExport:
		if response.Export == nil {
			return fmt.Errorf("export payload is required")
		}
		if response.SendNudge != nil || response.Read != nil || response.Subscribe != nil || response.Cancel != nil || response.Event != nil {
			return fmt.Errorf("export response contains unsupported payloads")
		}
		return response.Export.Validate()
	case MachineOpCancel:
		if response.Cancel == nil {
			return fmt.Errorf("cancel payload is required")
		}
		if response.SendNudge != nil || response.Read != nil || response.Subscribe != nil || response.Export != nil || response.Event != nil {
			return fmt.Errorf("cancel response contains unsupported payloads")
		}
		return response.Cancel.Validate()
	default:
		return fmt.Errorf("unsupported operation %q", response.Operation)
	}
}

func (request MachineSendNudgeRequest) Validate() error {
	if err := validateMachineIdentifier(request.DeliveryID, "delivery_id", MachineMaxDeliveryBytes); err != nil {
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
	if request.OriginMessageID != "" {
		if err := validateMachineIdentifier(request.OriginMessageID, "origin_message_id", MachineMaxCursorBytes); err != nil {
			return err
		}
	}
	if len(request.CauseEventIDs) > 0 {
		if err := validateMachineStringSlice(request.CauseEventIDs, MachineMaxCauseEventIDs, MachineMaxCauseBytes, "cause_event_ids"); err != nil {
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
		if err := validateMachineIdentifier(request.Before, "before", MachineMaxCursorBytes); err != nil {
			return err
		}
	}
	if request.After != "" {
		if err := validateMachineIdentifier(request.After, "after", MachineMaxCursorBytes); err != nil {
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
	if request.ResumeCursor != "" {
		if err := validateMachineIdentifier(request.ResumeCursor, "resume_cursor", MachineMaxCursorBytes); err != nil {
			return err
		}
	}
	return nil
}

func (request MachineExportRequest) Validate() error {
	if err := validateMachineStringSlice(request.RoomIDs, MachineMaxExportRoomTargets, MachineMaxTargetBytes, "room_ids"); err != nil {
		return err
	}
	if err := validateMachineStringSlice(request.DMPeerIDs, MachineMaxExportPeerTargets, MachineMaxTargetBytes, "dm_peer_ids"); err != nil {
		return err
	}
	if len(request.RoomIDs) == 0 && len(request.DMPeerIDs) == 0 {
		return fmt.Errorf("at least one of room_ids or dm_peer_ids is required")
	}
	if request.IncludeSocial == nil {
		return fmt.Errorf("include_social_speech is required")
	}
	return nil
}

func (request MachineCancelRequest) Validate() error {
	return validateMachineIdentifier(request.TargetCorrelationID, "target_correlation_id", MachineMaxCorrelationBytes)
}

func (result MachineSendNudgeResult) Validate() error {
	if result.Accepted == nil {
		return fmt.Errorf("accepted is required")
	}
	if result.ThreadCreated == nil {
		return fmt.Errorf("thread_created is required")
	}
	if result.DMCreated == nil {
		return fmt.Errorf("dm_created is required")
	}
	if *result.ThreadCreated && result.ThreadID == "" {
		return fmt.Errorf("thread_id is required when thread_created is true")
	}
	if !*result.ThreadCreated && result.ThreadID != "" {
		return fmt.Errorf("thread_id must be empty when thread_created is false")
	}
	if *result.DMCreated && result.DMID == "" {
		return fmt.Errorf("dm_id is required when dm_created is true")
	}
	if !*result.DMCreated && result.DMID != "" {
		return fmt.Errorf("dm_id must be empty when dm_created is false")
	}
	if err := validateMachineIdentifier(result.MessageID, "message_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if err := validateMachineIdentifier(result.EventID, "event_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if result.ThreadID != "" {
		if err := validateMachineIdentifier(result.ThreadID, "thread_id", MachineMaxTargetBytes); err != nil {
			return err
		}
	}
	if result.DMID != "" {
		if err := validateMachineIdentifier(result.DMID, "dm_id", MachineMaxTargetBytes); err != nil {
			return err
		}
	}
	return nil
}

func (result MachineSubscribeResult) Validate() error {
	if result.Closed != MachineSubscribeClosed {
		return fmt.Errorf("closed must be %q", MachineSubscribeClosed)
	}
	switch result.Reason {
	case MachineSubscribeReasonDone, MachineSubscribeReasonLimit, MachineSubscribeReasonEOF:
		return nil
	default:
		return fmt.Errorf("reason must be one of done, limit, or eof")
	}
}

func (event MachineSubscribeEvent) Validate() error {
	if err := validateMachineIdentifier(event.EventID, "event_id", MachineMaxTargetBytes); err != nil {
		return err
	}
	if event.Type == "" {
		return fmt.Errorf("type is required")
	}
	if strings.TrimSpace(event.Type) != event.Type {
		return fmt.Errorf("type must not include leading or trailing whitespace")
	}
	if len(event.Type) > MachineMaxTargetBytes {
		return fmt.Errorf("type must be at most %d bytes", MachineMaxTargetBytes)
	}
	if len(event.Payload) == 0 {
		return fmt.Errorf("payload is required")
	}
	if len(event.Payload) > MachineMaxOutputLineBytes {
		return fmt.Errorf("payload must be at most %d bytes", MachineMaxOutputLineBytes)
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
	if strings.ToLower(result.TranscriptSHA) != result.TranscriptSHA {
		return fmt.Errorf("transcript_sha256 must be lowercase")
	}
	actual := sha256.Sum256([]byte(result.Transcript))
	expected := strings.ToLower(hex.EncodeToString(actual[:]))
	if result.TranscriptSHA != expected {
		return fmt.Errorf("transcript_sha256 does not match transcript")
	}
	return nil
}

func (result MachineCancelResult) Validate() error {
	if err := validateMachineIdentifier(result.TargetCorrelationID, "target_correlation_id", MachineMaxCorrelationBytes); err != nil {
		return err
	}
	switch result.State {
	case MachineCancelStateCanceled, MachineCancelStateAlreadyFinal, MachineCancelStateNotFound:
		return nil
	default:
		return fmt.Errorf("state must be canceled, already_final, or not_found")
	}
}

func (target MachineTarget) Validate() error {
	switch target.Kind {
	case MachineTargetKindRoom, MachineTargetKindDM:
	default:
		return fmt.Errorf("kind must be room or dm")
	}
	if err := validateMachineIdentifier(target.ID, "target.id", MachineMaxTargetBytes); err != nil {
		return err
	}
	return nil
}

func (errorResponse MachineError) Validate() error {
	if strings.TrimSpace(errorResponse.Code) == "" {
		return fmt.Errorf("error code is required")
	}
	switch errorResponse.Code {
	case MachineErrorInvalidRequest, MachineErrorDuplicateRequest, MachineErrorUnsupported, MachineErrorNotFound,
		MachineErrorConflict, MachineErrorCapacity, MachineErrorTransport, MachineErrorCanceled:
		return nil
	default:
		return fmt.Errorf("unknown error code %q", errorResponse.Code)
	}
}
