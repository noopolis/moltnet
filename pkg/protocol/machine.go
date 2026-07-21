package protocol

import "encoding/json"

const (
	MachineProtocolV1          = "moltnet.machine.v1"
	MachineExportMarker        = "moltnet.control.nudge.v1"
	MachineExportSchemaVersion = "moltnet.machine.export.v1"

	MachineMaxInputLineBytes      = 16384
	MachineMaxOutputLineBytes     = 16384
	MachineMaxCorrelationBytes    = 128
	MachineMaxDeliveryBytes       = 128
	MachineMaxTargetBytes         = 128
	MachineMaxBodyBytes           = 2048
	MachineMaxCauseBytes          = 128
	MachineMaxCursorBytes         = 128
	MachineMaxTranscriptBytes     = 262144
	MachineMaxExportRoomTargets   = 32
	MachineMaxExportPeerTargets   = 32
	MachineMaxReadLimit           = 128
	MachineMaxSubscribeEvents     = 256
	MachineMaxActiveRequests      = 512
	MachineMaxCorrelationRegistry = 1024
	MachineMaxDeliveryRegistry    = 1024
	MachineMaxCauseEventIDs       = 32
)

const (
	MachineOpSendNudge = "send_nudge"
	MachineOpRead      = "read"
	MachineOpSubscribe = "subscribe"
	MachineOpExport    = "export"
	MachineOpCancel    = "cancel"
)

const (
	MachineTargetKindRoom = "room"
	MachineTargetKindDM   = "dm"
)

const (
	MachineErrorInvalidRequest   = "invalid_request"
	MachineErrorDuplicateRequest = "duplicate_request"
	MachineErrorUnsupported      = "unsupported"
	MachineErrorNotFound         = "not_found"
	MachineErrorConflict         = "conflict"
	MachineErrorCapacity         = "capacity"
	MachineErrorTransport        = "transport"
	MachineErrorCanceled         = "canceled"
)

const (
	MachineSubscribeClosed      = "closed"
	MachineSubscribeReasonDone  = "done"
	MachineSubscribeReasonLimit = "limit"
	MachineSubscribeReasonEOF   = "eof"
)

const (
	MachineCancelStateCanceled     = "canceled"
	MachineCancelStateAlreadyFinal = "already_final"
	MachineCancelStateNotFound     = "not_found"
)

type MachineRequest struct {
	Version       string                   `json:"version"`
	CorrelationID string                   `json:"correlation_id"`
	Operation     string                   `json:"operation"`
	SendNudge     *MachineSendNudgeRequest `json:"send_nudge,omitempty"`
	Read          *MachineReadRequest      `json:"read,omitempty"`
	Subscribe     *MachineSubscribeRequest `json:"subscribe,omitempty"`
	Export        *MachineExportRequest    `json:"export,omitempty"`
	Cancel        *MachineCancelRequest    `json:"cancel,omitempty"`
}

type MachineResponse struct {
	Version       string                  `json:"version"`
	CorrelationID string                  `json:"correlation_id"`
	Operation     string                  `json:"operation"`
	SendNudge     *MachineSendNudgeResult `json:"send_nudge,omitempty"`
	Read          *MachineReadResult      `json:"read,omitempty"`
	Subscribe     *MachineSubscribeResult `json:"subscribe,omitempty"`
	Export        *MachineExportResult    `json:"export,omitempty"`
	Cancel        *MachineCancelResult    `json:"cancel,omitempty"`
	Event         *MachineSubscribeEvent  `json:"event,omitempty"`
	Error         *MachineError           `json:"error,omitempty"`
}

type MachineError struct {
	Code string `json:"code"`
}

type MachineTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type MachineSendNudgeRequest struct {
	DeliveryID      string        `json:"delivery_id"`
	Target          MachineTarget `json:"target"`
	Body            string        `json:"body"`
	OriginMessageID string        `json:"origin_message_id,omitempty"`
	CauseEventIDs   []string      `json:"cause_event_ids,omitempty"`
}

type MachineSendNudgeResult struct {
	MessageID     string `json:"message_id"`
	EventID       string `json:"event_id"`
	Accepted      *bool  `json:"accepted"`
	ThreadID      string `json:"thread_id,omitempty"`
	ThreadCreated *bool  `json:"thread_created"`
	DMID          string `json:"dm_id,omitempty"`
	DMCreated     *bool  `json:"dm_created"`
}

type MachineReadRequest struct {
	Target MachineTarget `json:"target"`
	Limit  int           `json:"limit"`
	Before string        `json:"before,omitempty"`
	After  string        `json:"after,omitempty"`
}

type MachineReadResult struct {
	Target MachineTarget   `json:"target"`
	Page   MachineReadPage `json:"page"`
}

type MachineReadPageInfo struct {
	HasMore    *bool  `json:"has_more"`
	NextBefore string `json:"next_before,omitempty"`
	NextAfter  string `json:"next_after,omitempty"`
}

type MachineReadMessage Message

type MachineReadPage struct {
	Messages []MachineReadMessage `json:"messages"`
	Page     MachineReadPageInfo  `json:"page"`
}

type MachineSubscribeRequest struct {
	Target       MachineTarget `json:"target"`
	ResumeCursor string        `json:"resume_cursor,omitempty"`
	MaxEvents    int           `json:"max_events"`
}

type MachineSubscribeResult struct {
	Closed string `json:"closed"`
	Reason string `json:"reason"`
}

type MachineSubscribeEvent struct {
	EventID string          `json:"event_id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type MachineExportRequest struct {
	RoomIDs       []string `json:"room_ids"`
	DMPeerIDs     []string `json:"dm_peer_ids"`
	IncludeSocial *bool    `json:"include_social_speech"`
}

type MachineExportResult struct {
	Version       string `json:"version"`
	ControlMarker string `json:"control_marker"`
	Transcript    string `json:"transcript"`
	TranscriptSHA string `json:"transcript_sha256"`
}

type MachineCancelRequest struct {
	TargetCorrelationID string `json:"target_correlation_id"`
}

type MachineCancelResult struct {
	TargetCorrelationID string `json:"target_correlation_id"`
	State               string `json:"state"`
}
