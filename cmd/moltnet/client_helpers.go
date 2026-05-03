package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type targetRef struct {
	id   string
	kind string
}

func loadClientConfig(explicitPath string) (clientconfig.Config, string, error) {
	path, ok, err := clientconfig.DiscoverPath(explicitPath)
	if err != nil {
		return clientconfig.Config{}, "", err
	}
	if !ok {
		return clientconfig.Config{}, "", fmt.Errorf("moltnet client config not found")
	}

	config, err := clientconfig.LoadFile(path)
	if err != nil {
		return clientconfig.Config{}, "", err
	}

	return config, path, nil
}

func resolveClient(explicitPath string, networkID string) (clientconfig.Config, clientconfig.AttachmentConfig, *moltnetclient.Client, error) {
	return resolveClientForMember(explicitPath, networkID, "")
}

func resolveClientForMember(
	explicitPath string,
	networkID string,
	memberID string,
) (clientconfig.Config, clientconfig.AttachmentConfig, *moltnetclient.Client, error) {
	config, _, err := loadClientConfig(explicitPath)
	if err != nil {
		return clientconfig.Config{}, clientconfig.AttachmentConfig{}, nil, err
	}

	attachment, err := config.ResolveAttachmentFor(networkID, memberID)
	if err != nil {
		return clientconfig.Config{}, clientconfig.AttachmentConfig{}, nil, err
	}

	client, err := moltnetclient.New(attachment)
	if err != nil {
		return clientconfig.Config{}, clientconfig.AttachmentConfig{}, nil, err
	}

	return config, attachment, client, nil
}

func flagWasSet(flags *flag.FlagSet, name string) bool {
	wasSet := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			wasSet = true
		}
	})
	return wasSet
}

func mergeAuthFromConfig(
	existing clientconfig.AuthConfig,
	requested clientconfig.AuthConfig,
	authModeSet bool,
	sourceExplicit bool,
) clientconfig.AuthConfig {
	if sourceExplicit {
		return requested
	}
	if !authModeSet {
		return existing
	}

	requestedMode := authMode(requested)
	existingMode := authMode(existing)
	if requestedMode != existingMode && requestedMode != bridgeconfig.AuthModeOpen {
		return requested
	}
	if !existing.HasTokenSource() {
		return requested
	}
	requested.Token = existing.Token
	requested.TokenEnv = existing.TokenEnv
	requested.TokenPath = existing.TokenPath
	return requested
}

func authMode(auth clientconfig.AuthConfig) string {
	mode := strings.TrimSpace(auth.Mode)
	if mode != "" {
		return mode
	}
	if auth.HasTokenSource() {
		return bridgeconfig.AuthModeBearer
	}
	return bridgeconfig.AuthModeNone
}

func applyAgentTokenToAuth(auth clientconfig.AuthConfig, token string) clientconfig.AuthConfig {
	auth.Mode = bridgeconfig.AuthModeOpen
	if strings.TrimSpace(auth.TokenEnv) != "" || strings.TrimSpace(auth.TokenPath) != "" {
		return auth
	}
	auth.Token = strings.TrimSpace(token)
	return auth
}

func printJSON(value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func parseTarget(value string) (targetRef, error) {
	kind, id, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || strings.TrimSpace(id) == "" {
		return targetRef{}, fmt.Errorf("target must be room:<id> or dm:<id>")
	}

	switch kind {
	case protocol.TargetKindRoom, protocol.TargetKindDM:
	default:
		return targetRef{}, fmt.Errorf("unsupported target kind %q", kind)
	}

	return targetRef{
		id:   strings.TrimSpace(id),
		kind: kind,
	}, nil
}

func ensureTargetAllowed(attachment clientconfig.AttachmentConfig, target targetRef) error {
	switch target.kind {
	case protocol.TargetKindRoom:
		for _, room := range attachment.Rooms {
			if room.ID == target.id {
				return nil
			}
		}
		return fmt.Errorf("room %q is not attached for member %q", target.id, attachment.MemberID)
	case protocol.TargetKindDM:
		if attachment.DMs == nil || !attachment.DMs.Enabled {
			return fmt.Errorf("direct messages are not enabled for member %q", attachment.MemberID)
		}
		return nil
	default:
		return fmt.Errorf("unsupported target kind %q", target.kind)
	}
}

func buildFromActor(attachment clientconfig.AttachmentConfig) protocol.Actor {
	return protocol.Actor{
		Type:      "agent",
		ID:        attachment.MemberID,
		Name:      attachment.AgentName,
		NetworkID: attachment.NetworkID,
	}
}

func clientConfigPathForWorkspace(workspace string) string {
	return clientconfig.DefaultPathForWorkspace(workspace)
}

func loadOrCreateClientConfig(path string, runtime string, agentName string) (clientconfig.Config, error) {
	if _, err := os.Stat(path); err == nil {
		return clientconfig.LoadFile(path)
	} else if err != nil && !os.IsNotExist(err) {
		return clientconfig.Config{}, fmt.Errorf("inspect Moltnet client config %q: %w", path, err)
	}

	return clientconfig.Config{
		Version: clientconfig.VersionV1,
		Agent: clientconfig.AgentConfig{
			Name:    agentName,
			Runtime: runtime,
		},
	}, nil
}

func upsertAttachment(config *clientconfig.Config, attachment clientconfig.AttachmentConfig) {
	for index, existing := range config.Attachments {
		if existing.NetworkID == attachment.NetworkID && existing.MemberID == attachment.MemberID {
			config.Attachments[index] = attachment
			return
		}
	}

	config.Attachments = append(config.Attachments, attachment)
}

func parseRooms(value string) []bridgeconfig.RoomBinding {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	rooms := make([]bridgeconfig.RoomBinding, 0)
	for _, roomID := range strings.Split(value, ",") {
		roomID = strings.TrimSpace(roomID)
		if roomID == "" {
			continue
		}
		rooms = append(rooms, bridgeconfig.RoomBinding{ID: roomID, Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyManual})
	}

	return rooms
}

func writeClientConfig(path string, config clientconfig.Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if err := validateClientConfigWriteTarget(path); err != nil {
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

func validateClientConfigWriteTarget(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat Moltnet client config %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Moltnet client config %q must not be a symlink when writing tokens", path)
	}
	if info.IsDir() {
		return fmt.Errorf("Moltnet client config %q is a directory", path)
	}
	return nil
}

type conversationsView struct {
	DMs       []protocol.DirectConversation `json:"dms,omitempty"`
	MemberID  string                        `json:"member_id"`
	NetworkID string                        `json:"network_id"`
	Rooms     []protocol.Room               `json:"rooms,omitempty"`
}

func commandContext() context.Context {
	return context.Background()
}
