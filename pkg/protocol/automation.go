package protocol

import "time"

// MessageWake is minted by the local server, never accepted from senders.
// Recipients overrides a bridge's static wake policy for this message.
type MessageWake struct {
	Recipients   []string `json:"recipients"`
	DiscussionID string   `json:"discussion_id,omitempty"`
	Turn         int      `json:"turn,omitempty"`
	MaxTurns     int      `json:"max_turns,omitempty"`
}

type AgentWakeSetting struct {
	AgentID string `json:"agent_id"`
	Mode    string `json:"mode"`
}

type Discussion struct {
	ID              string    `json:"id"`
	Active          bool      `json:"active"`
	Topic           string    `json:"topic"`
	Participants    []string  `json:"participants"`
	MaxRounds       int       `json:"max_rounds"`
	CompletedTurns  int       `json:"completed_turns"`
	ExpectedAgent   string    `json:"expected_agent,omitempty"`
	ExpectedReplyTo string    `json:"expected_reply_to,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type StartDiscussionRequest struct {
	Topic        string   `json:"topic"`
	Participants []string `json:"participants"`
	MaxRounds    int      `json:"max_rounds"`
}

type WebhookSetting struct {
	URL           string `json:"url"`
	Key           string `json:"key,omitempty"`
	Enabled       bool   `json:"enabled"`
	MaxRunsPerDay int    `json:"max_runs_per_day"`
}

// ConnectorStatus never exposes the webhook URL or secret.
type ConnectorStatus struct {
	Verified      bool   `json:"verified"`
	AgentID       string `json:"agent_id"`
	Configured    bool   `json:"configured"`
	Enabled       bool   `json:"enabled"`
	MaxRunsPerDay int    `json:"max_runs_per_day"`
	AttemptsToday int    `json:"attempts_today"`
	LastStatus    string `json:"last_status,omitempty"`
}

type RoomAutomation struct {
	Settings   []AgentWakeSetting `json:"settings"`
	Discussion *Discussion        `json:"discussion,omitempty"`
	Connectors []ConnectorStatus  `json:"connectors"`
}
