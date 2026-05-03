package clisession

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

var workspaceConfigLocks sync.Map

func EnsureWorkspaceClientConfig(config bridgeconfig.Config) error {
	workspace := strings.TrimSpace(config.Runtime.WorkspacePath)
	if workspace == "" {
		return nil
	}

	token, _, err := config.Moltnet.ResolveToken()
	if err != nil {
		return err
	}

	path := clientconfig.DefaultPathForWorkspace(workspace)
	lock := workspaceConfigLock(path)
	lock.Lock()
	defer lock.Unlock()

	clientConfig, err := loadWorkspaceClientConfig(path, config)
	if err != nil {
		return err
	}

	upsertWorkspaceAttachment(&clientConfig, runtimeAttachment(config, token))
	return writeWorkspaceClientConfig(path, clientConfig)
}

func loadWorkspaceClientConfig(path string, config bridgeconfig.Config) (clientconfig.Config, error) {
	if _, err := os.Stat(path); err == nil {
		return clientconfig.LoadFile(path)
	} else if err != nil && !os.IsNotExist(err) {
		return clientconfig.Config{}, fmt.Errorf("inspect Moltnet client config %q: %w", path, err)
	}

	return clientconfig.Config{
		Version: clientconfig.VersionV1,
		Agent: clientconfig.AgentConfig{
			Name:    bridgeutil.DisplayName(config.Agent),
			Runtime: config.Runtime.Kind,
		},
	}, nil
}

func runtimeAttachment(config bridgeconfig.Config, token string) clientconfig.AttachmentConfig {
	mode := config.Moltnet.EffectiveAuthMode()
	auth := clientconfig.AuthConfig{Mode: mode}
	if mode != bridgeconfig.AuthModeNone && strings.TrimSpace(token) != "" {
		auth.Token = strings.TrimSpace(token)
	}

	return clientconfig.AttachmentConfig{
		AgentName: bridgeutil.DisplayName(config.Agent),
		Auth:      auth,
		BaseURL:   strings.TrimSpace(config.Moltnet.BaseURL),
		DMs:       config.DMs,
		MemberID:  strings.TrimSpace(config.Agent.ID),
		NetworkID: strings.TrimSpace(config.Moltnet.NetworkID),
		Rooms:     append([]bridgeconfig.RoomBinding(nil), config.Rooms...),
		Runtime:   strings.TrimSpace(config.Runtime.Kind),
	}
}

func upsertWorkspaceAttachment(config *clientconfig.Config, attachment clientconfig.AttachmentConfig) {
	if config.Agent.Name == "" {
		config.Agent.Name = attachment.AgentName
	}
	if config.Agent.Runtime == "" {
		config.Agent.Runtime = attachment.Runtime
	}

	for index, existing := range config.Attachments {
		if existing.NetworkID == attachment.NetworkID && existing.MemberID == attachment.MemberID {
			config.Attachments[index] = attachment
			return
		}
	}
	config.Attachments = append(config.Attachments, attachment)
}

func writeWorkspaceClientConfig(path string, config clientconfig.Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create Moltnet client config directory: %w", err)
	}

	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Moltnet client config: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temporary Moltnet client config: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temporary Moltnet client config: %w", err)
	}
	if _, err := temp.Write(append(payload, '\n')); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary Moltnet client config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary Moltnet client config: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace Moltnet client config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod Moltnet client config: %w", err)
	}
	return nil
}

func workspaceConfigLock(path string) *sync.Mutex {
	lockPath, err := filepath.Abs(path)
	if err != nil {
		lockPath = path
	}
	value, _ := workspaceConfigLocks.LoadOrStore(lockPath, &sync.Mutex{})
	return value.(*sync.Mutex)
}
