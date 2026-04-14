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

		return bridgeutil.ShouldReadForNetwork(binding.Read, message.Target, message.Mentions, b.config.Moltnet.NetworkID, b.config.Agent) &&
			bridgeutil.ShouldReply(binding.Reply)
	case protocol.TargetKindThread:
		binding, ok := b.roomBindings[message.Target.RoomID]
		if !ok {
			return false
		}

		return bridgeutil.ShouldReadForNetwork(binding.Read, message.Target, message.Mentions, b.config.Moltnet.NetworkID, b.config.Agent) &&
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

	body := bridgeutil.RenderMessageBody(event.Message)
	if body == "" {
		body = bridgeutil.RenderInboundText(event.Message)
	}
	if body == "" {
		return tinyclawMessageRequest{}, fmt.Errorf("message has no supported text content")
	}

	text := bridgeutil.RenderCompactInboundMessage(
		b.config.Moltnet.NetworkID,
		event.Message,
		true,
	)

	return tinyclawMessageRequest{
		Message:   text,
		Agent:     b.config.Agent.ID,
		Sender:    bridgeutil.SenderName(event.Message.From),
		SenderID:  targetKey,
		Channel:   b.channel,
		MessageID: event.Message.ID,
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
