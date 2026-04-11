package openclaw

import (
	"fmt"
	"strings"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const openClawSessionAgentID = "main"

func buildInboundMessage(config bridgeconfig.Config, event protocol.Event) (string, error) {
	if event.Message == nil {
		return "", fmt.Errorf("event has no message")
	}

	return bridgeutil.RenderCompactInboundMessage(
		config.Moltnet.NetworkID,
		event.Message,
		true,
	), nil
}

func buildBootstrapMessage(config bridgeconfig.Config, target protocol.Target) string {
	return bridgeutil.RenderCompactBootstrapMessage(config.Moltnet.NetworkID, target, true)
}

func sessionKey(config bridgeconfig.Config, message *protocol.Message) string {
	contextID := conversationContextID(config.Moltnet.NetworkID, message)
	return sessionKeyFromContext(contextID)
}

func sessionKeyForTarget(config bridgeconfig.Config, target protocol.Target) string {
	contextID := conversationContextIDForTarget(config.Moltnet.NetworkID, target)
	return sessionKeyFromContext(contextID)
}

func sessionKeyFromContext(contextID string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(contextID), "moltnet:")
	if trimmed == "" {
		trimmed = "main"
	}
	return fmt.Sprintf("agent:%s:moltnet:%s", openClawSessionAgentID, trimmed)
}

func idempotencyKey(config bridgeconfig.Config, event protocol.Event) string {
	if strings.TrimSpace(event.ID) == "" {
		return ""
	}
	return fmt.Sprintf("moltnet:%s:%s", config.Agent.ID, strings.TrimSpace(event.ID))
}

func bootstrapIdempotencyKey(config bridgeconfig.Config, target protocol.Target) string {
	contextID := conversationContextIDForTarget(config.Moltnet.NetworkID, target)
	if contextID == "" {
		return ""
	}

	return fmt.Sprintf("moltnet:%s:bootstrap:%s", config.Agent.ID, contextID)
}

func conversationContextID(networkID string, message *protocol.Message) string {
	if message == nil {
		return ""
	}

	return conversationContextIDForTarget(networkID, message.Target, message.ID)
}

func conversationContextIDForTarget(networkID string, target protocol.Target, fallbackMessageID ...string) string {
	switch target.Kind {
	case protocol.TargetKindRoom:
		if target.RoomID != "" {
			return fmt.Sprintf("moltnet:%s:room:%s", networkID, target.RoomID)
		}
	case protocol.TargetKindDM:
		if target.DMID != "" {
			return fmt.Sprintf("moltnet:%s:dm:%s", networkID, target.DMID)
		}
	case protocol.TargetKindThread:
		if target.ThreadID != "" {
			return fmt.Sprintf("moltnet:%s:thread:%s", networkID, target.ThreadID)
		}
	}

	if len(fallbackMessageID) == 0 || fallbackMessageID[0] == "" {
		return ""
	}

	return fmt.Sprintf("moltnet:%s:%s", networkID, fallbackMessageID[0])
}
