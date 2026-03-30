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

	EventTypeMessageCreated = "message.created"
)

type Network struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	Capabilities NetworkCapabilities `json:"capabilities,omitempty"`
}

type NetworkCapabilities struct {
	EventStream       string `json:"event_stream,omitempty"`
	HumanIngress      bool   `json:"human_ingress"`
	MessagePagination string `json:"message_pagination,omitempty"`
	Pairings          bool   `json:"pairings"`
}

type Actor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type Target struct {
	Kind           string   `json:"kind"`
	RoomID         string   `json:"room_id,omitempty"`
	ThreadID       string   `json:"thread_id,omitempty"`
	DMID           string   `json:"dm_id,omitempty"`
	ParticipantIDs []string `json:"participant_ids,omitempty"`
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
	ID        string    `json:"id"`
	NetworkID string    `json:"network_id"`
	Target    Target    `json:"target"`
	From      Actor     `json:"from"`
	Parts     []Part    `json:"parts"`
	Mentions  []string  `json:"mentions,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	NetworkID string    `json:"network_id"`
	Message   *Message  `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Room struct {
	ID        string    `json:"id"`
	FQID      string    `json:"fqid,omitempty"`
	Name      string    `json:"name"`
	Members   []string  `json:"members,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type DirectConversation struct {
	ID             string    `json:"id"`
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
	ID                string `json:"id"`
	RemoteNetworkID   string `json:"remote_network_id"`
	RemoteNetworkName string `json:"remote_network_name,omitempty"`
	Status            string `json:"status,omitempty"`
}

type CreateRoomRequest struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Members []string `json:"members,omitempty"`
}

type SendMessageRequest struct {
	ID       string   `json:"id,omitempty"`
	Target   Target   `json:"target"`
	From     Actor    `json:"from"`
	Parts    []Part   `json:"parts"`
	Mentions []string `json:"mentions,omitempty"`
}

type MessageAccepted struct {
	MessageID string `json:"message_id"`
	EventID   string `json:"event_id"`
	Accepted  bool   `json:"accepted"`
}

type PageInfo struct {
	HasMore    bool   `json:"has_more"`
	NextBefore string `json:"next_before,omitempty"`
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
	case TargetKindThread:
		if strings.TrimSpace(target.ThreadID) == "" {
			return fmt.Errorf("target.thread_id is required for thread messages")
		}
	case TargetKindDM:
		if strings.TrimSpace(target.DMID) == "" {
			return fmt.Errorf("target.dm_id is required for direct messages")
		}
		participants := uniqueParticipantIDs(target.ParticipantIDs)
		if len(participants) < 2 {
			return fmt.Errorf("target.participant_ids must declare at least two direct-message participants")
		}
	default:
		return fmt.Errorf("unsupported target kind %q", target.Kind)
	}

	return nil
}

func uniqueParticipantIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	participants := make([]string, 0, len(values))

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}

		if _, ok := seen[trimmed]; ok {
			continue
		}

		seen[trimmed] = struct{}{}
		participants = append(participants, trimmed)
	}

	return participants
}
