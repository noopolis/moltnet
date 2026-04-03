package clientconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	VersionV1     = "moltnet.client.v1"
	LegacyVersion = "spawnfile.moltnet-http-skill.v1"
	DefaultPath   = ".moltnet/config.json"
	LegacyPath    = ".spawnfile/moltnet.json"
)

var DefaultDiscoveryOrder = []string{
	DefaultPath,
	LegacyPath,
}

type Config struct {
	Version     string             `json:"version"`
	Agent       AgentConfig        `json:"agent"`
	Attachments []AttachmentConfig `json:"attachments"`
}

type AgentConfig struct {
	Name    string `json:"name,omitempty"`
	Runtime string `json:"runtime,omitempty"`
}

type AttachmentConfig struct {
	AgentName string                     `json:"agent_name,omitempty"`
	Auth      AuthConfig                 `json:"auth"`
	BaseURL   string                     `json:"base_url"`
	DMs       *bridgeconfig.DMConfig     `json:"dms,omitempty"`
	MemberID  string                     `json:"member_id"`
	NetworkID string                     `json:"network_id"`
	Rooms     []bridgeconfig.RoomBinding `json:"rooms,omitempty"`
	Runtime   string                     `json:"runtime,omitempty"`
}

type AuthConfig struct {
	Mode     string `json:"mode"`
	Token    string `json:"token,omitempty"`
	TokenEnv string `json:"token_env,omitempty"`
}

func LoadFile(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read Moltnet client config: %w", err)
	}

	var config Config
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode Moltnet client config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return Config{}, err
	}

	return config, nil
}

func DiscoverPath(explicit string) (string, bool, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return statPath(value)
	}
	if value := strings.TrimSpace(os.Getenv("MOLTNET_CLIENT_CONFIG")); value != "" {
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
	version := strings.TrimSpace(c.Version)
	switch version {
	case "", VersionV1, LegacyVersion:
	default:
		return fmt.Errorf("unsupported Moltnet client version %q", version)
	}

	if len(c.Attachments) == 0 {
		return fmt.Errorf("attachments must contain at least one entry")
	}

	seen := make(map[string]struct{}, len(c.Attachments))
	for index, attachment := range c.Attachments {
		if err := attachment.Validate(); err != nil {
			return fmt.Errorf("attachments[%d]: %w", index, err)
		}

		key := attachment.NetworkID + "::" + attachment.MemberID
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate attachment for network %q and member %q", attachment.NetworkID, attachment.MemberID)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func (a AttachmentConfig) Validate() error {
	if strings.TrimSpace(a.BaseURL) == "" {
		return fmt.Errorf("base_url is required")
	}
	if strings.TrimSpace(a.NetworkID) == "" {
		return fmt.Errorf("network_id is required")
	}
	if strings.TrimSpace(a.MemberID) == "" {
		return fmt.Errorf("member_id is required")
	}
	if err := protocol.ValidateMemberID(strings.TrimSpace(a.MemberID)); err != nil {
		return fmt.Errorf("member_id %w", err)
	}
	for index, room := range a.Rooms {
		if strings.TrimSpace(room.ID) == "" {
			return fmt.Errorf("rooms[%d].id is required", index)
		}
		if err := protocol.ValidateRoomID(strings.TrimSpace(room.ID)); err != nil {
			return fmt.Errorf("rooms[%d].id %w", index, err)
		}
	}

	switch strings.TrimSpace(a.Auth.Mode) {
	case "", "none":
	case "bearer":
		if strings.TrimSpace(a.Auth.Token) == "" && strings.TrimSpace(a.Auth.TokenEnv) == "" {
			return fmt.Errorf("auth.token or auth.token_env is required for bearer auth")
		}
	default:
		return fmt.Errorf("unsupported auth.mode %q", a.Auth.Mode)
	}

	return nil
}

func (a AttachmentConfig) ResolveToken() (string, error) {
	if strings.TrimSpace(a.Auth.Mode) == "" || strings.TrimSpace(a.Auth.Mode) == "none" {
		return "", nil
	}
	if token := strings.TrimSpace(a.Auth.Token); token != "" {
		return token, nil
	}
	if envName := strings.TrimSpace(a.Auth.TokenEnv); envName != "" {
		token := strings.TrimSpace(os.Getenv(envName))
		if token == "" {
			return "", fmt.Errorf("environment variable %q is required for Moltnet bearer auth", envName)
		}
		return token, nil
	}

	return "", fmt.Errorf("unsupported auth configuration")
}

func (c Config) ResolveAttachment(networkID string) (AttachmentConfig, error) {
	if len(c.Attachments) == 1 && strings.TrimSpace(networkID) == "" {
		return c.Attachments[0], nil
	}

	if strings.TrimSpace(networkID) == "" {
		return AttachmentConfig{}, fmt.Errorf("multiple Moltnet attachments are configured; --network is required")
	}

	for _, attachment := range c.Attachments {
		if attachment.NetworkID == strings.TrimSpace(networkID) {
			return attachment, nil
		}
	}

	return AttachmentConfig{}, fmt.Errorf("no Moltnet attachment configured for network %q", networkID)
}

func statPath(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("Moltnet client config %q is a directory", path)
	}

	return path, true, nil
}

func DefaultPathForWorkspace(workspace string) string {
	root := strings.TrimSpace(workspace)
	if root == "" {
		root = "."
	}
	return filepath.Join(root, ".moltnet", "config.json")
}
