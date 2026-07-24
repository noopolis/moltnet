package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type machineRequestFixture struct {
	name    string
	request MachineRequest
}

type machineResponseFixture struct {
	name     string
	response MachineResponse
}

func machineBool(value bool) *bool { return &value }

func machineTargetFixture() MachineTarget {
	return MachineTarget{Kind: MachineTargetKindRoom, ID: "room_1"}
}

func machineReadPageFixture(cursor string, after bool) MachineReadPage {
	message := MachineReadMessage(Message{
		ID: "msg_2", NetworkID: "net_1",
		Origin:   MessageOrigin{NetworkID: "net_1", MessageID: "msg_1"},
		Target:   Target{Kind: TargetKindRoom, RoomID: "room_1"},
		From:     Actor{Type: "agent", ID: "agent_1"},
		Parts:    []Part{{Kind: PartKindText, Text: "hello"}},
		Mentions: []string{"agent_2"}, CreatedAt: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC),
	})
	info := MachineReadPageInfo{HasMore: machineBool(true)}
	if after {
		info.NextAfter = cursor
	} else {
		info.NextBefore = cursor
	}
	return MachineReadPage{Messages: []MachineReadMessage{message}, Page: info}
}

func machineRequestFixtures() []machineRequestFixture {
	return []machineRequestFixture{
		{"send_nudge_request", MachineRequest{
			Version: MachineProtocolV1, CorrelationID: "corr_send_1", Operation: MachineOpSendNudge,
			SendNudge: &MachineSendNudgeRequest{
				DeliveryID: "delivery_1", Target: machineTargetFixture(), Body: "wake for nudge",
				OriginMessageID: "origin_1", CauseEventIDs: []string{"ev_1", "ev_2"},
			},
		}},
		{"read_request", MachineRequest{
			Version: MachineProtocolV1, CorrelationID: "corr_read_1", Operation: MachineOpRead,
			Read: &MachineReadRequest{Target: machineTargetFixture(), Limit: 20, After: "msg_1"},
		}},
		{"subscribe_request", MachineRequest{
			Version: MachineProtocolV1, CorrelationID: "corr_sub_1", Operation: MachineOpSubscribe,
			Subscribe: &MachineSubscribeRequest{Target: machineTargetFixture(), ResumeCursor: "cursor_1", MaxEvents: 25},
		}},
		{"export_request", MachineRequest{
			Version: MachineProtocolV1, CorrelationID: "corr_exp_1", Operation: MachineOpExport,
			Export: &MachineExportRequest{
				RoomIDs: []string{"room_1", "room_2"}, DMPeerIDs: []string{"peer_1"},
				IncludeSocial: machineBool(true),
			},
		}},
		{"cancel_request", MachineRequest{
			Version: MachineProtocolV1, CorrelationID: "corr_can_1", Operation: MachineOpCancel,
			Cancel: &MachineCancelRequest{TargetCorrelationID: "corr_read_1"},
		}},
	}
}

func machineErrorFixture(name, correlationID, operation, code string) machineResponseFixture {
	return machineResponseFixture{name, MachineResponse{
		Version: MachineProtocolV1, CorrelationID: correlationID, Operation: operation,
		Error: &MachineError{Code: code},
	}}
}

func machineSubscribeClosedFixture(name, correlationID, reason string) machineResponseFixture {
	return machineResponseFixture{name, MachineResponse{
		Version: MachineProtocolV1, CorrelationID: correlationID, Operation: MachineOpSubscribe,
		Subscribe: &MachineSubscribeResult{Closed: MachineSubscribeClosed, Reason: reason},
	}}
}

func machineResponseFixtures() []machineResponseFixture {
	transcript := "one\nline"
	transcriptHash := sha256.Sum256([]byte(transcript))
	return []machineResponseFixture{
		{"send_nudge_success", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_send_1", Operation: MachineOpSendNudge,
			SendNudge: &MachineSendNudgeResult{
				MessageID: "message_1", EventID: "event_1", Accepted: machineBool(true),
				ThreadID: "thread_1", ThreadCreated: machineBool(true), DMCreated: machineBool(false),
			},
		}},
		{"read_success_nonempty_with_after", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_read_1", Operation: MachineOpRead,
			Read: &MachineReadResult{Target: machineTargetFixture(), Page: machineReadPageFixture("msg_3", true)},
		}},
		{"read_success_nonempty_with_before", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_read_2", Operation: MachineOpRead,
			Read: &MachineReadResult{Target: machineTargetFixture(), Page: machineReadPageFixture("msg_1", false)},
		}},
		{"read_success_empty", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_read_3", Operation: MachineOpRead,
			Read: &MachineReadResult{Target: machineTargetFixture(), Page: MachineReadPage{
				Page: MachineReadPageInfo{HasMore: machineBool(false)},
			}},
		}},
		{"subscribe_event", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_sub_1", Operation: MachineOpSubscribe,
			Event: &MachineSubscribeEvent{EventID: "e1", Type: "message", Payload: json.RawMessage(`{"message":"m"}`)},
		}},
		machineSubscribeClosedFixture("subscribe_done", "corr_sub_2", MachineSubscribeReasonDone),
		machineSubscribeClosedFixture("subscribe_limit", "corr_sub_3", MachineSubscribeReasonLimit),
		machineSubscribeClosedFixture("subscribe_eof", "corr_sub_4", MachineSubscribeReasonEOF),
		{"export_success", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_exp_1", Operation: MachineOpExport,
			Export: &MachineExportResult{
				Version: MachineExportSchemaVersion, ControlMarker: MachineExportMarker,
				Transcript: transcript, TranscriptSHA: hex.EncodeToString(transcriptHash[:]),
			},
		}},
		machineErrorFixture("export_unsupported", "corr_exp_2", MachineOpExport, MachineErrorUnsupported),
		machineErrorFixture("subscribe_unsupported", "corr_sub_5", MachineOpSubscribe, MachineErrorUnsupported),
		machineErrorFixture("error_invalid_request", "corr_err_1", MachineOpSendNudge, MachineErrorInvalidRequest),
		machineErrorFixture("error_duplicate_request", "corr_err_2", MachineOpSendNudge, MachineErrorDuplicateRequest),
		machineErrorFixture("error_not_found", "corr_err_3", MachineOpSendNudge, MachineErrorNotFound),
		machineErrorFixture("error_conflict", "corr_err_4", MachineOpSendNudge, MachineErrorConflict),
		machineErrorFixture("error_capacity", "corr_err_5", MachineOpSendNudge, MachineErrorCapacity),
		machineErrorFixture("error_transport", "corr_err_6", MachineOpSendNudge, MachineErrorTransport),
		machineErrorFixture("error_canceled", "corr_err_7", MachineOpSendNudge, MachineErrorCanceled),
		{"cancel_success", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_can_1", Operation: MachineOpCancel,
			Cancel: &MachineCancelResult{TargetCorrelationID: "corr_read_1", State: MachineCancelStateCanceled},
		}},
		{"cancel_already_final", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_can_2", Operation: MachineOpCancel,
			Cancel: &MachineCancelResult{TargetCorrelationID: "corr_read_1", State: MachineCancelStateAlreadyFinal},
		}},
		{"cancel_not_found", MachineResponse{
			Version: MachineProtocolV1, CorrelationID: "corr_can_3", Operation: MachineOpCancel,
			Cancel: &MachineCancelResult{TargetCorrelationID: "corr_read_1", State: MachineCancelStateNotFound},
		}},
	}
}
