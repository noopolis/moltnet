package loop

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

var mentionPattern = regexp.MustCompile(`@([A-Za-z0-9._-]+)`)

func ShouldHandle(config bridgeconfig.Config, event protocol.Event) bool {
	if event.Type != protocol.EventTypeMessageCreated || event.Message == nil {
		return false
	}

	message := event.Message
	if message.NetworkID != config.Moltnet.NetworkID || message.From.ID == config.Agent.ID {
		return false
	}

	switch message.Target.Kind {
	case protocol.TargetKindRoom:
		for _, binding := range config.Rooms {
			if binding.ID == message.Target.RoomID {
				return shouldRead(binding.Read, message.Mentions, config.Agent) && shouldReply(binding.Reply)
			}
		}
		return false
	case protocol.TargetKindDM:
		return shouldHandleDirectMessage(config, message)
	default:
		return false
	}
}

func RenderInboundText(message *protocol.Message) string {
	if message == nil {
		return ""
	}

	prefix := targetPrefix(message.Target, senderName(message.From))
	lines := make([]string, 0, len(message.Parts)+1)

	for _, part := range message.Parts {
		switch part.Kind {
		case "text":
			if text := strings.TrimSpace(part.Text); text != "" {
				lines = append(lines, text)
			}
		case "url":
			if text := strings.TrimSpace(part.URL); text != "" {
				lines = append(lines, text)
			}
		case "data":
			if payload, ok := renderDataPart(part.Data); ok {
				lines = append(lines, payload)
			}
		}
	}

	if len(lines) == 0 {
		return ""
	}

	return strings.TrimSpace(strings.Join(append([]string{prefix}, lines...), "\n"))
}

func parseMentions(text string) []string {
	matches := mentionPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	mentions := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 || strings.TrimSpace(match[1]) == "" {
			continue
		}

		if _, ok := seen[match[1]]; ok {
			continue
		}

		seen[match[1]] = struct{}{}
		mentions = append(mentions, match[1])
	}

	return mentions
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

func shouldHandleDirectMessage(config bridgeconfig.Config, message *protocol.Message) bool {
	if config.DMs == nil || !config.DMs.Enabled || !shouldReadDirect(config.DMs.Read) || !shouldReply(config.DMs.Reply) {
		return false
	}

	for _, participantID := range message.Target.ParticipantIDs {
		if participantID == config.Agent.ID || participantID == config.Agent.Name {
			return true
		}
	}

	return false
}

func shouldReply(mode bridgeconfig.ReplyConfig) bool {
	switch mode {
	case "", bridgeconfig.ReplyAuto:
		return true
	default:
		return false
	}
}

func senderName(actor protocol.Actor) string {
	if strings.TrimSpace(actor.Name) != "" {
		return actor.Name
	}
	return actor.ID
}

func displayName(agent bridgeconfig.AgentConfig) string {
	if strings.TrimSpace(agent.Name) != "" {
		return agent.Name
	}
	return agent.ID
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

func renderDataPart(data map[string]any) (string, bool) {
	if len(data) == 0 {
		return "", false
	}

	files, ok := data["files"]
	if !ok {
		return "", false
	}

	values, ok := files.([]any)
	if !ok {
		return "", false
	}

	names := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if ok && strings.TrimSpace(text) != "" {
			names = append(names, text)
		}
	}

	if len(names) == 0 {
		return "", false
	}

	return "files: " + strings.Join(names, ", "), true
}
