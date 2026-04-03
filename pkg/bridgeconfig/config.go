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
	Version string        `json:"version" yaml:"version"`
	Agent   AgentConfig   `json:"agent" yaml:"agent"`
	Moltnet MoltnetConfig `json:"moltnet" yaml:"moltnet"`
	Runtime RuntimeConfig `json:"runtime" yaml:"runtime"`
	Read    ReadConfig    `json:"read,omitempty" yaml:"read,omitempty"`
	Reply   ReplyConfig   `json:"reply,omitempty" yaml:"reply,omitempty"`
	Rooms   []RoomBinding `json:"rooms,omitempty" yaml:"rooms,omitempty"`
	DMs     *DMConfig     `json:"dms,omitempty" yaml:"dms,omitempty"`
}

type AgentConfig struct {
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

type MoltnetConfig struct {
	BaseURL   string `json:"base_url" yaml:"base_url"`
	NetworkID string `json:"network_id" yaml:"network_id"`
	Token     string `json:"token,omitempty" yaml:"token,omitempty"`
}

type RuntimeConfig struct {
	Kind        string `json:"kind" yaml:"kind"`
	Token       string `json:"token,omitempty" yaml:"token,omitempty"`
	Channel     string `json:"channel,omitempty" yaml:"channel,omitempty"`
	InboundURL  string `json:"inbound_url,omitempty" yaml:"inbound_url,omitempty"`
	OutboundURL string `json:"outbound_url,omitempty" yaml:"outbound_url,omitempty"`
	AckURL      string `json:"ack_url,omitempty" yaml:"ack_url,omitempty"`
	EventsURL   string `json:"events_url,omitempty" yaml:"events_url,omitempty"`
	ControlURL  string `json:"control_url,omitempty" yaml:"control_url,omitempty"`
}

type RoomBinding struct {
	ID    string      `json:"id" yaml:"id"`
	Read  ReadConfig  `json:"read,omitempty" yaml:"read,omitempty"`
	Reply ReplyConfig `json:"reply,omitempty" yaml:"reply,omitempty"`
}

type DMConfig struct {
	Enabled bool        `json:"enabled" yaml:"enabled"`
	Read    ReadConfig  `json:"read,omitempty" yaml:"read,omitempty"`
	Reply   ReplyConfig `json:"reply,omitempty" yaml:"reply,omitempty"`
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
	if strings.TrimSpace(config.Moltnet.Token) != "" || strings.TrimSpace(config.Runtime.Token) != "" {
		if err := validatePrivateConfigMode(path); err != nil {
			return Config{}, err
		}
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

	if err := validateURL("bridge config moltnet.base_url", c.Moltnet.BaseURL); err != nil {
		return err
	}
	if err := validateReadReplyConfig(c); err != nil {
		return err
	}

	if strings.TrimSpace(c.Runtime.ControlURL) != "" {
		if err := validateURL("bridge config runtime.control_url", c.Runtime.ControlURL); err != nil {
			return err
		}
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

		if err := validateURL("bridge config runtime.inbound_url", c.Runtime.InboundURL); err != nil {
			return err
		}
		if err := validateURL("bridge config runtime.outbound_url", c.Runtime.OutboundURL); err != nil {
			return err
		}
		if err := validateURL("bridge config runtime.ack_url", c.Runtime.AckURL); err != nil {
			return err
		}
	}

	if c.Runtime.Kind == RuntimeOpenClaw || c.Runtime.Kind == RuntimePicoClaw {
		return fmt.Errorf("bridge config runtime.control_url is required for %s", c.Runtime.Kind)
	}

	return nil
}
