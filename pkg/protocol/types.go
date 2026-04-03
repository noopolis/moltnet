package protocol

import (
	"fmt"
	"strings"
	"time"
)

const (
	TargetKindRoom   = "room"
	TargetKindThread = "thread"
	TargetKindDM     = "dm"
)

type Network struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	Capabilities NetworkCapabilities `json:"capabilities,omitempty"`
}

type NetworkCapabilities struct {
	EventStream        string `json:"event_stream,omitempty"`
	AttachmentProtocol string `json:"attachment_protocol,omitempty"`
	HumanIngress       bool   `json:"human_ingress"`
	MessagePagination  string `json:"message_pagination,omitempty"`
	Pairings           bool   `json:"pairings"`
}

type Actor struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	NetworkID string `json:"network_id,omitempty"`
	FQID      string `json:"fqid,omitempty"`
}

type MessageOrigin struct {
	NetworkID string `json:"network_id"`
	MessageID string `json:"message_id"`
}

type Target struct {
	Kind            string   `json:"kind"`
	RoomID          string   `json:"room_id,omitempty"`
	ThreadID        string   `json:"thread_id,omitempty"`
	ParentMessageID string   `json:"parent_message_id,omitempty"`
	DMID            string   `json:"dm_id,omitempty"`
	ParticipantIDs  []string `json:"participant_ids,omitempty"`
}

type Part struct {
	Kind      string         `json:"kind"`
	Text      string         `json:"text,omitempty"`
	MediaType string         `json:"media_type,omitempty"`
	URL       string         `json:"url,omitempty"`
	Filename  string         `json:"filename,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type Message struct {
	ID        string        `json:"id"`
	NetworkID string        `json:"network_id"`
	Origin    MessageOrigin `json:"origin,omitempty"`
	Target    Target        `json:"target"`
	From      Actor         `json:"from"`
	Parts     []Part        `json:"parts"`
	Mentions  []string      `json:"mentions,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
}

type Event struct {
	ID        string              `json:"id"`
	Type      string              `json:"type"`
	NetworkID string              `json:"network_id"`
	Message   *Message            `json:"message,omitempty"`
	Room      *Room               `json:"room,omitempty"`
	Thread    *Thread             `json:"thread,omitempty"`
	DM        *DirectConversation `json:"dm,omitempty"`
	Pairing   *Pairing            `json:"pairing,omitempty"`
	ReplayGap *ReplayGap          `json:"replay_gap,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
}

type Room struct {
	ID        string    `json:"id"`
	NetworkID string    `json:"network_id,omitempty"`
	FQID      string    `json:"fqid,omitempty"`
	Name      string    `json:"name"`
	Members   []string  `json:"members,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type DirectConversation struct {
	ID             string    `json:"id"`
	NetworkID      string    `json:"network_id,omitempty"`
	FQID           string    `json:"fqid,omitempty"`
	ParticipantIDs []string  `json:"participant_ids,omitempty"`
	MessageCount   int       `json:"message_count"`
	LastMessageAt  time.Time `json:"last_message_at,omitempty"`
}

type AgentSummary struct {
	ID        string   `json:"id"`
	FQID      string   `json:"fqid,omitempty"`
	NetworkID string   `json:"network_id"`
	Rooms     []string `json:"rooms,omitempty"`
}

type Pairing struct {
	ID                string `json:"id" yaml:"id"`
	RemoteNetworkID   string `json:"remote_network_id" yaml:"remote_network_id"`
	RemoteNetworkName string `json:"remote_network_name,omitempty" yaml:"remote_network_name,omitempty"`
	RemoteBaseURL     string `json:"remote_base_url,omitempty" yaml:"remote_base_url,omitempty"`
	Status            string `json:"status,omitempty" yaml:"status,omitempty"`
	Token             string `json:"token,omitempty" yaml:"token,omitempty"`
}

type CreateRoomRequest struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Members []string `json:"members,omitempty"`
}

type UpdateRoomMembersRequest struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

type SendMessageRequest struct {
	ID       string        `json:"id,omitempty"`
	Origin   MessageOrigin `json:"origin,omitempty"`
	Target   Target        `json:"target"`
	From     Actor         `json:"from"`
	Parts    []Part        `json:"parts"`
	Mentions []string      `json:"mentions,omitempty"`
}

type MessageAccepted struct {
	MessageID     string `json:"message_id"`
	EventID       string `json:"event_id"`
	Accepted      bool   `json:"accepted"`
	ThreadCreated bool   `json:"thread_created"`
	DMCreated     bool   `json:"dm_created"`
}

type PageInfo struct {
	HasMore    bool   `json:"has_more"`
	NextBefore string `json:"next_before,omitempty"`
	NextAfter  string `json:"next_after,omitempty"`
}

type MessagePage struct {
	Messages []Message `json:"messages"`
	Page     PageInfo  `json:"page"`
}

func ValidateTarget(target Target) error {
	switch target.Kind {
	case TargetKindRoom:
		if strings.TrimSpace(target.RoomID) == "" {
			return fmt.Errorf("target.room_id is required for room messages")
		}
		if err := ValidateRoomID(strings.TrimSpace(target.RoomID)); err != nil {
			return fmt.Errorf("target.room_id %w", err)
		}
	case TargetKindThread:
		if strings.TrimSpace(target.ThreadID) == "" {
			return fmt.Errorf("target.thread_id is required for thread messages")
		}
		if strings.TrimSpace(target.RoomID) == "" {
			return fmt.Errorf("target.room_id is required for thread messages")
		}
		if err := ValidateRoomID(strings.TrimSpace(target.RoomID)); err != nil {
			return fmt.Errorf("target.room_id %w", err)
		}
		if err := ValidateMessageID(strings.TrimSpace(target.ThreadID)); err != nil {
			return fmt.Errorf("target.thread_id %w", err)
		}
		if parent := strings.TrimSpace(target.ParentMessageID); parent != "" {
			if err := ValidateMessageID(parent); err != nil {
				return fmt.Errorf("target.parent_message_id %w", err)
			}
		}
	case TargetKindDM:
		if strings.TrimSpace(target.DMID) == "" {
			return fmt.Errorf("target.dm_id is required for direct messages")
		}
		participants := UniqueTrimmedStrings(target.ParticipantIDs)
		if len(participants) < 2 {
			return fmt.Errorf("target.participant_ids must declare at least two direct-message participants")
		}
		if err := ValidateMessageID(strings.TrimSpace(target.DMID)); err != nil {
			return fmt.Errorf("target.dm_id %w", err)
		}
		if len(participants) > MaxMembersPerRequest {
			return fmt.Errorf("target.participant_ids must contain %d IDs or fewer", MaxMembersPerRequest)
		}
		for index, participantID := range participants {
			if err := ValidateMemberID(participantID); err != nil {
				return fmt.Errorf("target.participant_ids[%d] %w", index, err)
			}
		}
	default:
		return fmt.Errorf("unsupported target kind %q", target.Kind)
	}

	return nil
}
