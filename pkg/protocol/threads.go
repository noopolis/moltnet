package protocol

import "time"

type Thread struct {
	ID              string    `json:"id"`
	NetworkID       string    `json:"network_id,omitempty"`
	FQID            string    `json:"fqid,omitempty"`
	RoomID          string    `json:"room_id"`
	ParentMessageID string    `json:"parent_message_id,omitempty"`
	MessageCount    int       `json:"message_count"`
	LastMessageAt   time.Time `json:"last_message_at,omitempty"`
}
