package tinyclaw

import (
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (b *bridge) shouldHandle(event moltnetEvent) bool {
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

		return shouldRead(binding.Read, message.Mentions, b.config.Agent)
	case protocol.TargetKindDM:
		return b.config.DMs != nil && b.config.DMs.Enabled && shouldReadDirect(b.config.DMs.Read)
	default:
		return false
	}
}

func (b *bridge) toTinyClawMessage(event moltnetEvent) (tinyclawMessageRequest, error) {
	if event.Message == nil {
		return tinyclawMessageRequest{}, fmt.Errorf("event has no message")
	}

	targetKey, err := encodeTarget(event.Message.Target)
	if err != nil {
		return tinyclawMessageRequest{}, err
	}

	text := renderInboundText(event.Message)
	if text == "" {
		return tinyclawMessageRequest{}, fmt.Errorf("message has no supported text content")
	}

	return tinyclawMessageRequest{
		Message:   text,
		Agent:     b.config.Agent.ID,
		Sender:    displayActor(event.Message.From),
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
			Kind: "text",
			Text: text,
		})
	}

	if len(response.Files) > 0 {
		parts = append(parts, protocol.Part{
			Kind: "data",
			Data: map[string]any{
				"files": response.Files,
			},
		})
	}

	if len(parts) == 0 {
		return protocol.SendMessageRequest{}, fmt.Errorf("response has no message or files")
	}

	return protocol.SendMessageRequest{
		Target: target,
		From: protocol.Actor{
			Type: "agent",
			ID:   b.config.Agent.ID,
			Name: b.agentName,
		},
		Parts: parts,
	}, nil
}

func shouldRead(mode bridgeconfig.ReadConfig, mentions []string, agent bridgeconfig.AgentConfig) bool {
	switch mode {
	case "", bridgeconfig.ReadAll:
		return true
	case bridgeconfig.ReadMentions:
		for _, mention := range mentions {
			if mention == agent.ID || mention == agent.Name {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func shouldReadDirect(mode bridgeconfig.ReadConfig) bool {
	switch mode {
	case "", bridgeconfig.ReadAll, bridgeconfig.ReadMentions:
		return true
	default:
		return false
	}
}

func displayActor(actor protocol.Actor) string {
	if strings.TrimSpace(actor.Name) != "" {
		return actor.Name
	}

	return actor.ID
}

func renderInboundText(message *protocol.Message) string {
	if message == nil {
		return ""
	}

	prefix := targetPrefix(message.Target, displayActor(message.From))
	var bodyLines []string

	for _, part := range message.Parts {
		switch part.Kind {
		case "text":
			if text := strings.TrimSpace(part.Text); text != "" {
				bodyLines = append(bodyLines, text)
			}
		case "url":
			if url := strings.TrimSpace(part.URL); url != "" {
				bodyLines = append(bodyLines, url)
			}
		case "data":
			if len(part.Data) > 0 {
				if payload, ok := renderDataPart(part.Data); ok {
					bodyLines = append(bodyLines, payload)
				}
			}
		}
	}

	if len(bodyLines) == 0 {
		return ""
	}

	lines := append([]string{prefix}, bodyLines...)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderDataPart(data map[string]any) (string, bool) {
	files, ok := data["files"]
	if !ok {
		return "", false
	}

	switch value := files.(type) {
	case []string:
		if len(value) == 0 {
			return "", false
		}
		return "files: " + strings.Join(value, ", "), true
	case []any:
		names := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				names = append(names, text)
			}
		}
		if len(names) == 0 {
			return "", false
		}
		return "files: " + strings.Join(names, ", "), true
	default:
		return "", false
	}
}

func targetPrefix(target protocol.Target, sender string) string {
	switch target.Kind {
	case protocol.TargetKindRoom:
		return fmt.Sprintf("[room %s] %s", target.RoomID, sender)
	case protocol.TargetKindDM:
		return fmt.Sprintf("[dm] %s", sender)
	case protocol.TargetKindThread:
		return fmt.Sprintf("[thread %s] %s", target.ThreadID, sender)
	default:
		return sender
	}
}

func encodeTarget(target protocol.Target) (string, error) {
	switch target.Kind {
	case protocol.TargetKindRoom:
		return "room:" + target.RoomID, nil
	case protocol.TargetKindDM:
		return "dm:" + target.DMID, nil
	case protocol.TargetKindThread:
		return "thread:" + target.ThreadID, nil
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
		return protocol.Target{Kind: protocol.TargetKindThread, ThreadID: id}, nil
	default:
		return protocol.Target{}, fmt.Errorf("invalid target kind %q", kind)
	}
}
