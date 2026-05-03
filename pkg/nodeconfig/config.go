package nodeconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"go.yaml.in/yaml/v3"
)

const (
	VersionV1      = "moltnet.node.v1"
	DefaultPath    = "MoltnetNode"
	defaultJSONAlt = "moltnet-node.json"
	defaultYAMLAlt = "moltnet-node.yaml"
	defaultYMLAlt  = "moltnet-node.yml"
)

var DefaultDiscoveryOrder = []string{
	DefaultPath,
	defaultYAMLAlt,
	defaultYMLAlt,
	defaultJSONAlt,
}

type Config struct {
	Version     string                     `json:"version" yaml:"version"`
	Moltnet     bridgeconfig.MoltnetConfig `json:"moltnet" yaml:"moltnet"`
	Attachments []AttachmentConfig         `json:"attachments" yaml:"attachments"`
}

type AttachmentConfig struct {
	Agent   bridgeconfig.AgentConfig        `json:"agent" yaml:"agent"`
	Moltnet bridgeconfig.MoltnetTokenConfig `json:"moltnet,omitempty" yaml:"moltnet,omitempty"`
	Runtime bridgeconfig.RuntimeConfig      `json:"runtime" yaml:"runtime"`
	Read    bridgeconfig.ReadConfig         `json:"read,omitempty" yaml:"read,omitempty"`
	Reply   bridgeconfig.ReplyConfig        `json:"reply,omitempty" yaml:"reply,omitempty"`
	Rooms   []bridgeconfig.RoomBinding      `json:"rooms,omitempty" yaml:"rooms,omitempty"`
	DMs     *bridgeconfig.DMConfig          `json:"dms,omitempty" yaml:"dms,omitempty"`
}

func LoadFile(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read MoltnetNode config: %w", err)
	}

	var config Config
	if formatForPath(path) == "json" {
		decoder := json.NewDecoder(bytes.NewReader(contents))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&config)
	} else {
		decoder := yaml.NewDecoder(bytes.NewReader(contents))
		decoder.KnownFields(true)
		err = decoder.Decode(&config)
	}
	if err != nil {
		return Config{}, fmt.Errorf("decode MoltnetNode config: %w", err)
	}

	config = config.ResolveTokenPaths(filepath.Dir(path))
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	if config.hasPlaintextTokens() {
		if err := validatePrivateConfigMode(path); err != nil {
			return Config{}, err
		}
	}

	return config, nil
}

func DiscoverPath(explicit string) (string, bool, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return statPath(value)
	}

	for _, candidate := range DefaultDiscoveryOrder {
		path, ok, err := statPath(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", false, err
		}
		if ok {
			return path, true, nil
		}
	}

	return "", false, nil
}

func (c Config) Validate() error {
	if version := strings.TrimSpace(c.Version); version != "" && version != VersionV1 {
		return fmt.Errorf("unsupported MoltnetNode version %q", version)
	}

	if strings.TrimSpace(c.Moltnet.BaseURL) == "" {
		return fmt.Errorf("moltnet.base_url is required")
	}
	if strings.TrimSpace(c.Moltnet.NetworkID) == "" {
		return fmt.Errorf("moltnet.network_id is required")
	}
	if err := validateSharedOpenAuth(c.Moltnet); err != nil {
		return err
	}

	seenAgents := make(map[string]struct{}, len(c.Attachments))
	for index, attachment := range c.Attachments {
		agentID := strings.TrimSpace(attachment.Agent.ID)
		if agentID == "" {
			return fmt.Errorf("attachments[%d].agent.id is required", index)
		}
		if _, ok := seenAgents[agentID]; ok {
			return fmt.Errorf("duplicate attachment agent.id %q", agentID)
		}
		seenAgents[agentID] = struct{}{}

		if err := attachment.bridgeConfig(c.Moltnet).Validate(); err != nil {
			return fmt.Errorf("attachments[%d]: %w", index, err)
		}
	}

	return nil
}

func (c Config) BridgeConfigs() []bridgeconfig.Config {
	configs := make([]bridgeconfig.Config, 0, len(c.Attachments))
	for _, attachment := range c.Attachments {
		configs = append(configs, attachment.bridgeConfig(c.Moltnet))
	}

	return configs
}

func (c Config) ResolveTokenPaths(baseDir string) Config {
	c.Moltnet = c.Moltnet.ResolveTokenPath(baseDir)
	for index := range c.Attachments {
		c.Attachments[index].Moltnet = c.Attachments[index].Moltnet.ResolveTokenPath(baseDir)
	}
	return c
}

func (a AttachmentConfig) bridgeConfig(moltnet bridgeconfig.MoltnetConfig) bridgeconfig.Config {
	if a.Moltnet.HasTokenSource() {
		moltnet.Token = a.Moltnet.Token
		moltnet.TokenEnv = a.Moltnet.TokenEnv
		moltnet.TokenPath = a.Moltnet.TokenPath
	}

	return bridgeconfig.Config{
		Version: bridgeconfig.VersionV1,
		Agent:   a.Agent,
		Moltnet: moltnet,
		Runtime: a.Runtime,
		Read:    a.Read,
		Reply:   a.Reply,
		Rooms:   append([]bridgeconfig.RoomBinding(nil), a.Rooms...),
		DMs:     a.DMs,
	}.Normalized()
}

func (c Config) hasPlaintextTokens() bool {
	if strings.TrimSpace(c.Moltnet.Token) != "" {
		return true
	}
	for _, attachment := range c.Attachments {
		if strings.TrimSpace(attachment.Moltnet.Token) != "" {
			return true
		}
		if strings.TrimSpace(attachment.Runtime.Token) != "" {
			return true
		}
	}
	return false
}

func validateSharedOpenAuth(moltnet bridgeconfig.MoltnetConfig) error {
	if moltnet.EffectiveAuthMode() != bridgeconfig.AuthModeOpen || !moltnet.HasTokenSource() {
		return nil
	}
	if !moltnet.StaticToken {
		return fmt.Errorf("moltnet.static_token must be true when a shared open-mode token source is configured")
	}
	token, err := resolveSharedOpenStaticToken(moltnet)
	if err != nil {
		return err
	}
	if bridgeconfig.IsAgentToken(token) {
		return fmt.Errorf("shared open-mode token source must not contain a generated agent token")
	}
	return nil
}

func resolveSharedOpenStaticToken(moltnet bridgeconfig.MoltnetConfig) (string, error) {
	if token := strings.TrimSpace(moltnet.Token); token != "" {
		return token, nil
	}
	if envName := strings.TrimSpace(moltnet.TokenEnv); envName != "" {
		token := strings.TrimSpace(os.Getenv(envName))
		if token == "" {
			return "", fmt.Errorf("environment variable %q is required for shared open auth", envName)
		}
		return token, nil
	}
	if path := strings.TrimSpace(moltnet.TokenPath); path != "" {
		return bridgeconfig.ReadTokenFile(path)
	}
	return "", fmt.Errorf("moltnet.token, token_env, or token_path is required for shared open auth")
}

func formatForPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return "json"
	}

	return "yaml"
}

func statPath(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("MoltnetNode config %q is a directory", path)
	}

	return path, true, nil
}
