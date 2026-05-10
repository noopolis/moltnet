package protocol

const (
	EventTypeMessageCreated     = "message.created"
	EventTypeRoomCreated        = "room.created"
	EventTypeRoomMembersUpdated = "room.members.updated"
	EventTypeThreadCreated      = "thread.created"
	EventTypeDMCreated          = "dm.created"
	EventTypePairingUpdated     = "pairing.updated"
	EventTypeAgentConnected     = "agent.connected"
	EventTypeAgentDisconnected  = "agent.disconnected"
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
	AgentID   string  `json:"agent_id"`
	NetworkID string  `json:"network_id,omitempty"`
	FQID      string  `json:"fqid,omitempty"`
	Name      string  `json:"name,omitempty"`
	MessageID string  `json:"message_id,omitempty"`
	Reason    string  `json:"reason,omitempty"`
	Target    *Target `json:"target,omitempty"`
	Error     string  `json:"error,omitempty"`
}
