package bridgeconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	VersionV1 = "moltnet.bridge.v1"

	RuntimeTinyClaw   = "tinyclaw"
	RuntimeOpenClaw   = "openclaw"
	RuntimePicoClaw   = "picoclaw"
	RuntimeClaudeCode = "claude-code"
	RuntimeCodex      = "codex"
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
	Kind             string `json:"kind" yaml:"kind"`
	Token            string `json:"token,omitempty" yaml:"token,omitempty"`
	Channel          string `json:"channel,omitempty" yaml:"channel,omitempty"`
	Command          string `json:"command,omitempty" yaml:"command,omitempty"`
	ConfigPath       string `json:"config_path,omitempty" yaml:"config_path,omitempty"`
	HomePath         string `json:"home_path,omitempty" yaml:"home_path,omitempty"`
	GatewayURL       string `json:"gateway_url,omitempty" yaml:"gateway_url,omitempty"`
	InboundURL       string `json:"inbound_url,omitempty" yaml:"inbound_url,omitempty"`
	OutboundURL      string `json:"outbound_url,omitempty" yaml:"outbound_url,omitempty"`
	AckURL           string `json:"ack_url,omitempty" yaml:"ack_url,omitempty"`
	EventsURL        string `json:"events_url,omitempty" yaml:"events_url,omitempty"`
	ControlURL       string `json:"control_url,omitempty" yaml:"control_url,omitempty"`
	WorkspacePath    string `json:"workspace_path,omitempty" yaml:"workspace_path,omitempty"`
	SessionStorePath string `json:"session_store_path,omitempty" yaml:"session_store_path,omitempty"`
	SessionPrefix    string `json:"session_prefix,omitempty" yaml:"session_prefix,omitempty"`
	Driver           string `json:"driver,omitempty" yaml:"driver,omitempty"`
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

	config = config.Normalized()
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
	c = c.Normalized()

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
	case RuntimeTinyClaw, RuntimeOpenClaw, RuntimePicoClaw, RuntimeClaudeCode, RuntimeCodex:
	default:
		return fmt.Errorf("bridge config runtime.kind %q is unsupported", c.Runtime.Kind)
	}

	if err := validateURL("bridge config moltnet.base_url", c.Moltnet.BaseURL); err != nil {
		return err
	}
	if err := validateReadReplyConfig(c); err != nil {
		return err
	}

	if err := validateRuntimeFieldCompatibility(c.Runtime); err != nil {
		return err
	}

	return validateRuntimeSeam(c.Runtime)
}

func validateRuntimeFieldCompatibility(runtime RuntimeConfig) error {
	if strings.TrimSpace(runtime.GatewayURL) != "" {
		if runtime.Kind != RuntimeOpenClaw {
			return fmt.Errorf("bridge config runtime.gateway_url is only supported for openclaw")
		}
		if err := validateSocketURL("bridge config runtime.gateway_url", runtime.GatewayURL); err != nil {
			return err
		}
	}

	if strings.TrimSpace(runtime.ControlURL) != "" {
		if runtime.Kind == RuntimeOpenClaw {
			return fmt.Errorf("bridge config runtime.control_url is unsupported for openclaw; use runtime.gateway_url")
		}
		if runtime.Kind != RuntimePicoClaw && runtime.Kind != RuntimeTinyClaw {
			return fmt.Errorf("bridge config runtime.control_url is only supported for picoclaw or tinyclaw")
		}
		if err := validateURL("bridge config runtime.control_url", runtime.ControlURL); err != nil {
			return err
		}
	}

	if strings.TrimSpace(runtime.EventsURL) != "" {
		if runtime.Kind != RuntimePicoClaw {
			return fmt.Errorf("bridge config runtime.events_url is only supported for picoclaw")
		}
		if err := validateSocketURL("bridge config runtime.events_url", runtime.EventsURL); err != nil {
			return err
		}
	}

	if strings.TrimSpace(runtime.Command) != "" {
		if runtime.Kind != RuntimePicoClaw && runtime.Kind != RuntimeClaudeCode && runtime.Kind != RuntimeCodex {
			return fmt.Errorf("bridge config runtime.command is only supported for picoclaw, claude-code, or codex")
		}
		if runtime.Kind == RuntimePicoClaw && strings.TrimSpace(runtime.ConfigPath) == "" {
			return fmt.Errorf("bridge config runtime.config_path is required when runtime.command is set")
		}
	}

	if strings.TrimSpace(runtime.InboundURL) != "" && runtime.Kind != RuntimeTinyClaw {
		return fmt.Errorf("bridge config runtime.inbound_url is only supported for tinyclaw")
	}
	if strings.TrimSpace(runtime.InboundURL) != "" {
		if err := validateURL("bridge config runtime.inbound_url", runtime.InboundURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(runtime.OutboundURL) != "" && runtime.Kind != RuntimeTinyClaw {
		return fmt.Errorf("bridge config runtime.outbound_url is only supported for tinyclaw")
	}
	if strings.TrimSpace(runtime.OutboundURL) != "" {
		if err := validateURL("bridge config runtime.outbound_url", runtime.OutboundURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(runtime.AckURL) != "" && runtime.Kind != RuntimeTinyClaw {
		return fmt.Errorf("bridge config runtime.ack_url is only supported for tinyclaw")
	}
	if strings.TrimSpace(runtime.AckURL) != "" {
		if err := validateURL("bridge config runtime.ack_url", runtime.AckURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(runtime.ConfigPath) != "" && runtime.Kind != RuntimePicoClaw {
		return fmt.Errorf("bridge config runtime.config_path is only supported for picoclaw")
	}

	return nil
}

func validateRuntimeSeam(runtime RuntimeConfig) error {
	switch runtime.Kind {
	case RuntimeClaudeCode, RuntimeCodex:
		if strings.TrimSpace(runtime.WorkspacePath) == "" {
			return fmt.Errorf("bridge config runtime.workspace_path is required for %s", runtime.Kind)
		}
		return nil
	case RuntimeTinyClaw:
		if strings.TrimSpace(runtime.ControlURL) != "" {
			return nil
		}
		if strings.TrimSpace(runtime.InboundURL) == "" {
			return fmt.Errorf("bridge config runtime.inbound_url is required for tinyclaw")
		}

		if strings.TrimSpace(runtime.OutboundURL) == "" {
			return fmt.Errorf("bridge config runtime.outbound_url is required for tinyclaw")
		}

		if strings.TrimSpace(runtime.AckURL) == "" {
			return fmt.Errorf("bridge config runtime.ack_url is required for tinyclaw")
		}

		return nil
	case RuntimeOpenClaw:
		if strings.TrimSpace(runtime.GatewayURL) == "" {
			return fmt.Errorf("bridge config runtime.gateway_url is required for openclaw")
		}
		return nil
	case RuntimePicoClaw:
		if strings.TrimSpace(runtime.ControlURL) != "" ||
			strings.TrimSpace(runtime.EventsURL) != "" ||
			strings.TrimSpace(runtime.Command) != "" {
			return nil
		}
		return fmt.Errorf("bridge config runtime.control_url, runtime.events_url, or runtime.command is required for picoclaw")
	}

	return nil
}
