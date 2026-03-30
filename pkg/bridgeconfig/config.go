package bridgeconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	VersionV1 = "moltnet.bridge.v1"

	RuntimeTinyClaw = "tinyclaw"
	RuntimeOpenClaw = "openclaw"
	RuntimePicoClaw = "picoclaw"
)

type Config struct {
	Version string        `json:"version"`
	Agent   AgentConfig   `json:"agent"`
	Moltnet MoltnetConfig `json:"moltnet"`
	Runtime RuntimeConfig `json:"runtime"`
	Read    ReadConfig    `json:"read,omitempty"`
	Reply   ReplyConfig   `json:"reply,omitempty"`
	Batch   *BatchConfig  `json:"batch,omitempty"`
	Rooms   []RoomBinding `json:"rooms,omitempty"`
	DMs     *DMConfig     `json:"dms,omitempty"`
}

type AgentConfig struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type MoltnetConfig struct {
	BaseURL   string `json:"base_url"`
	NetworkID string `json:"network_id"`
	Token     string `json:"token,omitempty"`
}

type RuntimeConfig struct {
	Kind        string `json:"kind"`
	Channel     string `json:"channel,omitempty"`
	InboundURL  string `json:"inbound_url,omitempty"`
	OutboundURL string `json:"outbound_url,omitempty"`
	AckURL      string `json:"ack_url,omitempty"`
	EventsURL   string `json:"events_url,omitempty"`
	ControlURL  string `json:"control_url,omitempty"`
}

type RoomBinding struct {
	ID    string       `json:"id"`
	Read  ReadConfig   `json:"read,omitempty"`
	Reply ReplyConfig  `json:"reply,omitempty"`
	Batch *BatchConfig `json:"batch,omitempty"`
}

type DMConfig struct {
	Enabled bool         `json:"enabled"`
	Read    ReadConfig   `json:"read,omitempty"`
	Reply   ReplyConfig  `json:"reply,omitempty"`
	Batch   *BatchConfig `json:"batch,omitempty"`
}

type ReadConfig string

const (
	ReadAll        ReadConfig = "all"
	ReadMentions   ReadConfig = "mentions"
	ReadThreadOnly ReadConfig = "thread_only"
)

type ReplyConfig string

const (
	ReplyAuto   ReplyConfig = "auto"
	ReplyManual ReplyConfig = "manual"
	ReplyNever  ReplyConfig = "never"
)

type BatchConfig struct {
	MaxMessages int    `json:"max_messages,omitempty"`
	MaxWait     string `json:"max_wait,omitempty"`
}

func LoadFile(path string) (Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read bridge config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(bytes, &config); err != nil {
		return Config{}, fmt.Errorf("decode bridge config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return Config{}, err
	}

	return config, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("bridge config version is required")
	}

	if strings.TrimSpace(c.Agent.ID) == "" {
		return fmt.Errorf("bridge config agent.id is required")
	}

	if strings.TrimSpace(c.Moltnet.BaseURL) == "" {
		return fmt.Errorf("bridge config moltnet.base_url is required")
	}

	if strings.TrimSpace(c.Moltnet.NetworkID) == "" {
		return fmt.Errorf("bridge config moltnet.network_id is required")
	}

	switch c.Runtime.Kind {
	case RuntimeTinyClaw, RuntimeOpenClaw, RuntimePicoClaw:
	default:
		return fmt.Errorf("bridge config runtime.kind %q is unsupported", c.Runtime.Kind)
	}

	if strings.TrimSpace(c.Runtime.ControlURL) != "" {
		return nil
	}

	if c.Runtime.Kind == RuntimeTinyClaw {
		if strings.TrimSpace(c.Runtime.InboundURL) == "" {
			return fmt.Errorf("bridge config runtime.inbound_url is required for tinyclaw")
		}

		if strings.TrimSpace(c.Runtime.OutboundURL) == "" {
			return fmt.Errorf("bridge config runtime.outbound_url is required for tinyclaw")
		}

		if strings.TrimSpace(c.Runtime.AckURL) == "" {
			return fmt.Errorf("bridge config runtime.ack_url is required for tinyclaw")
		}
	}

	if c.Runtime.Kind == RuntimeOpenClaw || c.Runtime.Kind == RuntimePicoClaw {
		return fmt.Errorf("bridge config runtime.control_url is required for %s", c.Runtime.Kind)
	}

	return nil
}
