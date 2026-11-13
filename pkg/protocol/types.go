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

const (
	RoomVisibilityPrivate = "private"
	RoomVisibilityPublic  = "public"
)

const (
	RoomWritePolicyMembers          = "members"
	RoomWritePolicyRegisteredAgents = "registered_agents"
	RoomWritePolicyOperators        = "operators"
)

type Network struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	Protocols    NetworkProtocols    `json:"protocols,omitempty"`
	Capabilities NetworkCapabilities `json:"capabilities,omitempty"`
	Console      *NetworkConsole     `json:"console,omitempty"`
	Warnings     []NetworkWarning    `json:"warnings,omitempty"`
	// Identity is the network's public signing key, when it has one.
	Identity *NetworkIdentity `json:"identity,omitempty"`
}

type NetworkConsole struct {
	CanManageAutomation bool          `json:"can_manage_automation"`
	CanFollow           bool          `json:"can_follow"`
	CanSendHuman        bool          `json:"can_send_human"`
	Viewer              ConsoleViewer `json:"viewer"`
	CanReadPairings     bool          `json:"can_read_pairings"`
	CanReadDMs          bool          `json:"can_read_dms"`
	CanConnectAgent     bool          `json:"can_connect_agent"`
}

// ConsoleViewer identifies the caller without exposing credentials.
type ConsoleViewer struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

type NetworkCapabilities struct {
	LocalAutomation    bool   `json:"local_automation,omitempty"`
	EventStream        string `json:"event_stream,omitempty"`
	AttachmentProtocol string `json:"attachment_protocol,omitempty"`
	PublicRead         bool   `json:"public_read"`
	AgentRegistration  string `json:"agent_registration,omitempty"`
	DebugEvents        bool   `json:"debug_events,omitempty"`
	HumanIngress       bool   `json:"human_ingress"`
	DirectMessages     bool   `json:"direct_messages"`
	MessagePagination  string `json:"message_pagination,omitempty"`
	Pairings           bool   `json:"pairings"`
}

type Actor struct {
	Type            string `json:"type"`
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	NetworkID       string `json:"network_id,omitempty"`
	FQID            string `json:"fqid,omitempty"`
	CredentialBound bool   `json:"credential_bound,omitempty"`
}

type MessageOrigin struct {
	// ClaimedAuthor preserves untrusted author data from a follow import.
	ClaimedAuthor *Actor `json:"claimed_author,omitempty"`
	// These follow-import fields are origin testimony, never identity anchors.
	ClaimedNetworkID          string `json:"claimed_network_id,omitempty"`
	ClaimedMessageID          string `json:"claimed_message_id,omitempty"`
	ClaimedDiscoveryNetworkID string `json:"claimed_discovery_network_id,omitempty"`
	NetworkID                 string `json:"network_id"`
	MessageID                 string `json:"message_id"`
	// ReceivedVia is the local pairing id for an inbound relay, resolved
	// server-side from its credential, or "follow:<id>" for an engine import.
	// Follow imports stamp NetworkID with FollowNamespace and MessageID with the served ID.
	// Empty for locally-originated messages.
	//
	// This is the ONLY provenance on a relayed message the receiving
	// operator authored: NetworkID and the sender's own network id are
	// peer-supplied claims. It is never read from the wire (normalizeOrigin
	// overwrites it unconditionally) and never sent outbound (relayRequest
	// clears it), so it cannot become peer testimony in either direction.
	ReceivedVia string `json:"received_via,omitempty"`
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
	Wake         *MessageWake  `json:"wake,omitempty"`
	ReplyTo      string        `json:"reply_to,omitempty"`
	DiscussionID string        `json:"discussion_id,omitempty"`
	ID           string        `json:"id"`
	NetworkID    string        `json:"network_id"`
	Origin       MessageOrigin `json:"origin,omitempty"`
	Target       Target        `json:"target"`
	From         Actor         `json:"from"`
	Parts        []Part        `json:"parts"`
	Mentions     []string      `json:"mentions,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

type Event struct {
	ID        string              `json:"id"`
	Type      string              `json:"type"`
	NetworkID string              `json:"network_id"`
	Agent     *AgentEvent         `json:"agent,omitempty"`
	Message   *Message            `json:"message,omitempty"`
	Room      *Room               `json:"room,omitempty"`
	Thread    *Thread             `json:"thread,omitempty"`
	DM        *DirectConversation `json:"dm,omitempty"`
	Pairing   *Pairing            `json:"pairing,omitempty"`
	ReplayGap *ReplayGap          `json:"replay_gap,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
}

type Room struct {
	Follow        *RoomFollow     `json:"follow,omitempty"`
	ID            string          `json:"id"`
	NetworkID     string          `json:"network_id,omitempty"`
	FQID          string          `json:"fqid,omitempty"`
	Name          string          `json:"name"`
	Members       []string        `json:"members,omitempty"`
	Visibility    string          `json:"visibility,omitempty"`
	WritePolicy   string          `json:"write_policy,omitempty"`
	Federation    *RoomFederation `json:"federation,omitempty" yaml:"federation,omitempty"`
	Access        *RoomAccess     `json:"access,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	LastMessageAt *time.Time      `json:"last_message_at,omitempty"`
}

type RoomAccess struct {
	CanRead  bool   `json:"can_read"`
	CanWrite bool   `json:"can_write"`
	Reason   string `json:"reason,omitempty"`
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
	Name      string   `json:"name,omitempty"`
	ActorUID  string   `json:"actor_uid,omitempty"`
	FQID      string   `json:"fqid,omitempty"`
	NetworkID string   `json:"network_id"`
	Rooms     []string `json:"rooms,omitempty"`
	Connected bool     `json:"connected"`
}

type RegisterAgentRequest struct {
	RequestedAgentID string `json:"requested_agent_id,omitempty"`
	Name             string `json:"name,omitempty"`
}

type AgentRegistration struct {
	NetworkID     string    `json:"network_id"`
	AgentID       string    `json:"agent_id"`
	ActorUID      string    `json:"actor_uid"`
	ActorURI      string    `json:"actor_uri"`
	DisplayName   string    `json:"display_name,omitempty"`
	AgentToken    string    `json:"agent_token,omitempty"`
	CredentialKey string    `json:"-"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type Pairing struct {
	ID                string                 `json:"id" yaml:"id"`
	RemoteNetworkID   string                 `json:"remote_network_id" yaml:"remote_network_id"`
	RemoteNetworkName string                 `json:"remote_network_name,omitempty" yaml:"remote_network_name,omitempty"`
	RemoteBaseURL     string                 `json:"remote_base_url,omitempty" yaml:"remote_base_url,omitempty"`
	Relay             *PairingRelay          `json:"relay,omitempty" yaml:"relay,omitempty"`
	Status            string                 `json:"status,omitempty" yaml:"status,omitempty"`
	Diagnostics       *PairingDiagnostics    `json:"diagnostics,omitempty" yaml:"diagnostics,omitempty"`
	Delivery          *PairingDeliveryStatus `json:"delivery,omitempty" yaml:"delivery,omitempty"`
	DeliveryLimits    *PairingDeliveryLimits `json:"delivery_limits,omitempty" yaml:"delivery_limits,omitempty"`
	Token             SecretString           `json:"token,omitempty" yaml:"token,omitempty"`
	// Scope "rooms" makes this a room link: it carries only the hub rooms
	// whose control log admits its network. Room links never expose agent
	// lists or other rooms, and never carry DMs. Empty is a full pairing.
	Scope string `json:"scope,omitempty" yaml:"scope,omitempty"`
}

// PairingScopeRooms marks a room link (see Pairing.Scope).
const PairingScopeRooms = "rooms"

// IsRoomLink reports whether a pairing carries only hub rooms.
func (p Pairing) IsRoomLink() bool {
	return strings.TrimSpace(p.Scope) == PairingScopeRooms
}

type CreateRoomRequest struct {
	ID          string          `json:"id"`
	Name        string          `json:"name,omitempty"`
	Members     []string        `json:"members,omitempty"`
	Visibility  string          `json:"visibility,omitempty"`
	WritePolicy string          `json:"write_policy,omitempty"`
	Federation  *RoomFederation `json:"federation,omitempty" yaml:"federation,omitempty"`
	Credential  SecretString    `json:"credential,omitempty"`
}

type JoinRoomRequest struct {
	From       Actor        `json:"from"`
	Credential SecretString `json:"credential"`
}

type UpdateRoomMembersRequest struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

type RemoveResult struct {
	Removed bool   `json:"removed"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Mode    string `json:"mode"`
}

type SendMessageRequest struct {
	ReplyTo      string        `json:"reply_to,omitempty"`
	DiscussionID string        `json:"discussion_id,omitempty"`
	ID           string        `json:"id,omitempty"`
	Origin       MessageOrigin `json:"origin,omitempty"`
	Target       Target        `json:"target"`
	From         Actor         `json:"from"`
	Parts        []Part        `json:"parts"`
	Mentions     []string      `json:"mentions,omitempty"`
	// CauseEventIDs additively threads causal-event parentage (see
	// pkg/protocol/causal.go) through to the message.accepted event stamped
	// once this request durably lands. It is caller-supplied context, never
	// derived from it: harnesses such as daimon's turn harness set this, the
	// model never does.
	CauseEventIDs []string `json:"cause_event_ids,omitempty"`
	// Envelope is the origin network's signed statement of this message,
	// sent beside it on pairing relays (moltnet.envelope.v1). Receivers
	// accept it only from pair-scoped credentials; local callers cannot
	// supply one.
	Envelope *SignedEnvelope `json:"envelope,omitempty"`
	// Receipt is the room home's signed placement of a hub-room message,
	// sent only by a home forwarding to its members.
	Receipt *SignedRoomReceipt `json:"receipt,omitempty"`
}

type MessageAccepted struct {
	MessageID     string `json:"message_id"`
	EventID       string `json:"event_id"`
	Accepted      bool   `json:"accepted"`
	ThreadCreated bool   `json:"thread_created"`
	DMCreated     bool   `json:"dm_created"`
	// Receipt is the home's placement of a hub-room submission, returned to
	// the submitting member so its own copy learns its room seq.
	Receipt *SignedRoomReceipt `json:"receipt,omitempty"`
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
