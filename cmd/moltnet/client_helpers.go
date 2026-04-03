package main

import (
	"context"
	"encoding/json"
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
		return clientconfig.Config{}, "", fmt.Errorf("Moltnet client config not found")
	}

	config, err := clientconfig.LoadFile(path)
	if err != nil {
		return clientconfig.Config{}, "", err
	}

	return config, path, nil
}

func resolveClient(explicitPath string, networkID string) (clientconfig.Config, clientconfig.AttachmentConfig, *moltnetclient.Client, error) {
	config, _, err := loadClientConfig(explicitPath)
	if err != nil {
		return clientconfig.Config{}, clientconfig.AttachmentConfig{}, nil, err
	}

	attachment, err := config.ResolveAttachment(networkID)
	if err != nil {
		return clientconfig.Config{}, clientconfig.AttachmentConfig{}, nil, err
	}

	client, err := moltnetclient.New(attachment)
	if err != nil {
		return clientconfig.Config{}, clientconfig.AttachmentConfig{}, nil, err
	}

	return config, attachment, client, nil
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create Moltnet client config directory: %w", err)
	}

	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Moltnet client config: %w", err)
	}

	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write Moltnet client config: %w", err)
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
