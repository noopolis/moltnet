package tinyclaw

import (
	"fmt"
	"net/url"
	"strings"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (b *bridge) shouldHandle(event protocol.Event) bool {
	if event.Type != protocol.EventTypeMessageCreated || event.Message == nil {
		return false
	}

	message := event.Message
	if message.NetworkID != b.config.Moltnet.NetworkID {
		return false
	}

	if message.From.ID == b.config.Agent.ID {
		return false
	}

	switch message.Target.Kind {
	case protocol.TargetKindRoom:
		binding, ok := b.roomBindings[message.Target.RoomID]
		if !ok {
			return false
		}

		return bridgeutil.ShouldRead(binding.Read, message.Target, message.Mentions, b.config.Agent) &&
			bridgeutil.ShouldReply(binding.Reply)
	case protocol.TargetKindThread:
		binding, ok := b.roomBindings[message.Target.RoomID]
		if !ok {
			return false
		}

		return bridgeutil.ShouldRead(binding.Read, message.Target, message.Mentions, b.config.Agent) &&
			bridgeutil.ShouldReply(binding.Reply)
	case protocol.TargetKindDM:
		return b.config.DMs != nil &&
			b.config.DMs.Enabled &&
			bridgeutil.ShouldReadDirect(b.config.DMs.Read) &&
			bridgeutil.ShouldReply(b.config.DMs.Reply)
	default:
		return false
	}
}

func (b *bridge) toTinyClawMessage(event protocol.Event) (tinyclawMessageRequest, error) {
	if event.Message == nil {
		return tinyclawMessageRequest{}, fmt.Errorf("event has no message")
	}

	targetKey, err := encodeTarget(event.Message.Target)
	if err != nil {
		return tinyclawMessageRequest{}, err
	}

	text := bridgeutil.RenderInboundText(event.Message)
	if text == "" {
		return tinyclawMessageRequest{}, fmt.Errorf("message has no supported text content")
	}

	return tinyclawMessageRequest{
		Message:   text,
		Agent:     b.config.Agent.ID,
		Sender:    bridgeutil.SenderName(event.Message.From),
		SenderID:  targetKey,
		Channel:   b.channel,
		MessageID: event.Message.ID,
	}, nil
}

func (b *bridge) toMoltnetMessage(response tinyclawPendingResponse) (protocol.SendMessageRequest, error) {
	target, err := decodeTarget(response.SenderID)
	if err != nil {
		return protocol.SendMessageRequest{}, err
	}

	parts := make([]protocol.Part, 0, 2)
	if text := strings.TrimSpace(response.Message); text != "" {
		parts = append(parts, protocol.Part{
			Kind: protocol.PartKindText,
			Text: text,
		})
	}

	if len(response.Files) > 0 {
		parts = append(parts, protocol.Part{
			Kind: protocol.PartKindData,
			Data: map[string]any{
				"files": response.Files,
			},
		})
	}

	if len(parts) == 0 {
		return protocol.SendMessageRequest{}, fmt.Errorf("response has no message or files")
	}

	return protocol.SendMessageRequest{
		ID:     responseMessageID(b.config.Agent.ID, response.ID),
		Target: target,
		From: protocol.Actor{
			Type: "agent",
			ID:   b.config.Agent.ID,
			Name: b.agentName,
		},
		Parts: parts,
	}, nil
}

func encodeTarget(target protocol.Target) (string, error) {
	switch target.Kind {
	case protocol.TargetKindRoom:
		return "room:" + target.RoomID, nil
	case protocol.TargetKindDM:
		return "dm:" + target.DMID, nil
	case protocol.TargetKindThread:
		if strings.TrimSpace(target.RoomID) == "" {
			return "", fmt.Errorf("thread target requires room_id")
		}
		return strings.Join([]string{
			"thread",
			url.QueryEscape(target.RoomID),
			url.QueryEscape(target.ThreadID),
			url.QueryEscape(target.ParentMessageID),
		}, ":"), nil
	default:
		return "", fmt.Errorf("unsupported target kind %q", target.Kind)
	}
}

func decodeTarget(value string) (protocol.Target, error) {
	kind, id, ok := strings.Cut(value, ":")
	if !ok {
		return protocol.Target{}, fmt.Errorf("invalid target key %q", value)
	}

	switch kind {
	case protocol.TargetKindRoom:
		return protocol.Target{Kind: protocol.TargetKindRoom, RoomID: id}, nil
	case protocol.TargetKindDM:
		return protocol.Target{Kind: protocol.TargetKindDM, DMID: id}, nil
	case protocol.TargetKindThread:
		parts := strings.SplitN(value, ":", 4)
		if len(parts) == 4 {
			roomID, err := url.QueryUnescape(parts[1])
			if err != nil {
				return protocol.Target{}, fmt.Errorf("decode thread room_id: %w", err)
			}
			threadID, err := url.QueryUnescape(parts[2])
			if err != nil {
				return protocol.Target{}, fmt.Errorf("decode thread thread_id: %w", err)
			}
			parentMessageID, err := url.QueryUnescape(parts[3])
			if err != nil {
				return protocol.Target{}, fmt.Errorf("decode thread parent_message_id: %w", err)
			}
			return protocol.Target{
				Kind:            protocol.TargetKindThread,
				RoomID:          roomID,
				ThreadID:        threadID,
				ParentMessageID: parentMessageID,
			}, nil
		}
		return protocol.Target{Kind: protocol.TargetKindThread, ThreadID: id}, nil
	default:
		return protocol.Target{}, fmt.Errorf("invalid target kind %q", kind)
	}
}

func responseMessageID(agentID string, responseID pendingResponseID) string {
	return fmt.Sprintf("tinyclaw:%s:%s", strings.TrimSpace(agentID), responseID.String())
}
