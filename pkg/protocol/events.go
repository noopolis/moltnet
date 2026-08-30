package protocol

const (
	EventTypeMessageCreated     = "message.created"
	EventTypeRoomCreated        = "room.created"
	EventTypeRoomRemoved        = "room.removed"
	EventTypeRoomMembersUpdated = "room.members.updated"
	EventTypeThreadCreated      = "thread.created"
	EventTypeDMCreated          = "dm.created"
	EventTypePairingUpdated     = "pairing.updated"
	EventTypeAgentConnected     = "agent.connected"
	EventTypeAgentDisconnected  = "agent.disconnected"
	EventTypeAgentRemoved       = "agent.removed"
	EventTypeAgentWakeDelivered = "agent.wake.delivered"
	EventTypeAgentWakeFailed    = "agent.wake.failed"
	EventTypeReplayGap          = "stream.replay_gap"
)

type ReplayGap struct {
	RequestedEventID string `json:"requested_event_id,omitempty"`
	OldestEventID    string `json:"oldest_event_id,omitempty"`
	NewestEventID    string `json:"newest_event_id,omitempty"`
}

type AgentEvent struct {
	AgentID        string  `json:"agent_id"`
	NetworkID      string  `json:"network_id,omitempty"`
	FQID           string  `json:"fqid,omitempty"`
	Name           string  `json:"name,omitempty"`
	MessageID      string  `json:"message_id,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	Target         *Target `json:"target,omitempty"`
	Error          string  `json:"error,omitempty"`
	Attempts       int     `json:"attempts,omitempty"`
	Classification string  `json:"classification,omitempty"`
}

// Wake failure classifications reported by bridge control-delivery retry
// logic (internal/bridge/loop) alongside EventTypeAgentWakeFailed. A
// connection-death wake failure (internal/transport/attach.go) carries
// no classification at all (zero value): it is not a delivery-retry
// verdict, just "the socket died with this wake still pending".
const (
	WakeFailureClassificationPermanent      = "permanent"
	WakeFailureClassificationRetryExhausted = "retry_exhausted"
	WakeFailureClassificationRuntimeFailed  = "runtime_failed"
	WakeFailureClassificationRuntimeStopped = "runtime_stopped"
)

// WakeFailureDetails carries the retry evidence a bridge control-delivery
// attempt gathered before giving up on a wake, so it travels alongside the
// terminal agent.wake.failed event instead of only living in a log line.
// Connection-death wake failures (internal/transport/attach.go) pass the
// zero value: they never attempted a classified retry.
type WakeFailureDetails struct {
	Attempts       int    `json:"attempts,omitempty"`
	Classification string `json:"classification,omitempty"`
}

// ReportWakeFailedRequest is the bridge -> server wire body for
// POST /v1/agents/wake-failed: a bridge control-delivery loop reporting a
// genuinely permanent (or retry-budget-exhausted) wake failure for an
// event it received and already ACKed, without tearing down the
// attachment socket to do so. AgentID is checked against the caller's
// authenticated agent scope the same way internal/transport/attach.go
// checks a declared identify frame's agent.id (see auth.Claims.AllowsAgent).
type ReportWakeFailedRequest struct {
	AgentID        string `json:"agent_id"`
	Event          Event  `json:"event"`
	Error          string `json:"error,omitempty"`
	Attempts       int    `json:"attempts,omitempty"`
	Classification string `json:"classification,omitempty"`
}
