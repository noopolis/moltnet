package protocol

const (
	EventTypeMessageCreated     = "message.created"
	EventTypeRoomCreated        = "room.created"
	EventTypeRoomMembersUpdated = "room.members.updated"
	EventTypeThreadCreated      = "thread.created"
	EventTypeDMCreated          = "dm.created"
	EventTypePairingUpdated     = "pairing.updated"
	EventTypeReplayGap          = "stream.replay_gap"
)

type ReplayGap struct {
	RequestedEventID string `json:"requested_event_id,omitempty"`
	OldestEventID    string `json:"oldest_event_id,omitempty"`
	NewestEventID    string `json:"newest_event_id,omitempty"`
}
