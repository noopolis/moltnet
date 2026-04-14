package bridge

import (
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func ParseMentions(text string) []string {
	return protocol.ParseMentions(text)
}

func RenderInboundText(message *protocol.Message) string {
	if message == nil {
		return ""
	}

	prefix := TargetPrefix(message.Target, SenderName(message.From))
	lines := make([]string, 0, len(message.Parts)+1)

	for _, part := range message.Parts {
		switch part.Kind {
		case protocol.PartKindText:
			if text := strings.TrimSpace(part.Text); text != "" {
				lines = append(lines, text)
			}
		case protocol.PartKindURL:
			if text := strings.TrimSpace(part.URL); text != "" {
				lines = append(lines, text)
			}
		case protocol.PartKindData:
			if payload, ok := RenderDataPart(part.Data); ok {
				lines = append(lines, payload)
			}
		}
	}

	if len(lines) == 0 {
		return ""
	}

	return strings.TrimSpace(strings.Join(append([]string{prefix}, lines...), "\n"))
}

func RenderMessageBody(message *protocol.Message) string {
	if message == nil {
		return ""
	}

	lines := make([]string, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch part.Kind {
		case protocol.PartKindText:
			if text := strings.TrimSpace(part.Text); text != "" {
				lines = append(lines, text)
			}
		case protocol.PartKindURL:
			if text := strings.TrimSpace(part.URL); text != "" {
				lines = append(lines, text)
			}
		case protocol.PartKindData:
			if payload, ok := RenderDataPart(part.Data); ok {
				lines = append(lines, payload)
			}
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func RenderCompactBootstrapMessage(networkID string, target protocol.Target, includeSessionHeader bool) string {
	lines := make([]string, 0, 4)
	if includeSessionHeader {
		lines = append(lines,
			"Channel: moltnet",
			"Chat ID: "+ChatID(networkID, target),
			"",
		)
	}
	lines = append(lines, "Moltnet conversation attached. Use the `moltnet` skill in this conversation.")

	return strings.Join(lines, "\n")
}

func RenderCompactInboundMessage(
	networkID string,
	message *protocol.Message,
	includeSessionHeader bool,
) string {
	if message == nil {
		return ""
	}

	lines := make([]string, 0, 10)
	if includeSessionHeader {
		lines = append(lines,
			"Channel: moltnet",
			"Chat ID: "+ChatID(networkID, message.Target, message.ID),
		)
	}

	lines = append(lines,
		"From: "+ActorAddress(networkID, message.From),
		"Name: "+SenderName(message.From),
	)
	if len(message.Mentions) > 0 {
		lines = append(lines, "Mentions: "+strings.Join(message.Mentions, ", "))
	}
	if trimmed := strings.TrimSpace(message.ID); trimmed != "" {
		lines = append(lines, "Message ID: "+trimmed)
	}

	body := RenderMessageBody(message)
	if body == "" {
		body = RenderInboundText(message)
	}

	lines = append(lines, "", "Message:", body)
	return strings.Join(lines, "\n")
}

func ShouldRead(mode bridgeconfig.ReadConfig, target protocol.Target, mentions []string, agent bridgeconfig.AgentConfig) bool {
	return ShouldReadForNetwork(mode, target, mentions, "", agent)
}

func ShouldReadForNetwork(
	mode bridgeconfig.ReadConfig,
	target protocol.Target,
	mentions []string,
	networkID string,
	agent bridgeconfig.AgentConfig,
) bool {
	switch mode {
	case "", bridgeconfig.ReadAll:
		return true
	case bridgeconfig.ReadMentions:
		for _, mention := range mentions {
			if mention == agent.ID || mention == agent.Name ||
				(strings.TrimSpace(networkID) != "" && protocol.ActorMatches(networkID, agent.ID, mention)) {
				return true
			}
		}
		return false
	case bridgeconfig.ReadThreadOnly:
		return target.Kind == protocol.TargetKindThread
	default:
		return false
	}
}

func ShouldReadDirect(mode bridgeconfig.ReadConfig) bool {
	switch mode {
	case "", bridgeconfig.ReadAll, bridgeconfig.ReadMentions:
		return true
	default:
		return false
	}
}

func ShouldReply(mode bridgeconfig.ReplyConfig) bool {
	switch mode {
	case "", bridgeconfig.ReplyAuto:
		return true
	default:
		return false
	}
}

func SenderName(actor protocol.Actor) string {
	if strings.TrimSpace(actor.Name) != "" {
		return actor.Name
	}
	return actor.ID
}

func DisplayName(agent bridgeconfig.AgentConfig) string {
	if strings.TrimSpace(agent.Name) != "" {
		return agent.Name
	}
	return agent.ID
}

func TargetPrefix(target protocol.Target, sender string) string {
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

func ChatID(networkID string, target protocol.Target, fallbackMessageID ...string) string {
	switch target.Kind {
	case protocol.TargetKindRoom:
		if trimmed := strings.TrimSpace(target.RoomID); trimmed != "" {
			return fmt.Sprintf("%s:room:%s", networkID, trimmed)
		}
	case protocol.TargetKindDM:
		if trimmed := strings.TrimSpace(target.DMID); trimmed != "" {
			return fmt.Sprintf("%s:dm:%s", networkID, trimmed)
		}
	case protocol.TargetKindThread:
		if trimmed := strings.TrimSpace(target.ThreadID); trimmed != "" {
			return fmt.Sprintf("%s:thread:%s", networkID, trimmed)
		}
	}

	if len(fallbackMessageID) > 0 {
		if trimmed := strings.TrimSpace(fallbackMessageID[0]); trimmed != "" {
			return fmt.Sprintf("%s:message:%s", networkID, trimmed)
		}
	}

	return strings.TrimSpace(networkID)
}

func ActorAddress(networkID string, actor protocol.Actor) string {
	if trimmed := strings.TrimSpace(actor.FQID); trimmed != "" {
		return trimmed
	}

	parts := make([]string, 0, 3)
	if trimmed := strings.TrimSpace(networkID); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(actor.Type); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(actor.ID); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if len(parts) == 0 {
		return SenderName(actor)
	}
	return strings.Join(parts, "/")
}

func RenderDataPart(data map[string]any) (string, bool) {
	if len(data) == 0 {
		return "", false
	}

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
