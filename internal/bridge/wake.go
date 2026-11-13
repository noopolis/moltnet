package bridge

import (
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// ShouldWakeMessage evaluates sender-aware policies only on server-stored
// events. The server authenticates human ingress before emitting these events;
// a claimed display name (including "operator") never makes an agent human.
func ShouldWakeMessage(mode bridgeconfig.WakeConfig, message *protocol.Message, networkID string, agent bridgeconfig.AgentConfig) bool {
	if message == nil {
		return false
	}
	if message.Wake != nil {
		for _, id := range message.Wake.Recipients {
			if id == agent.ID {
				return true
			}
		}
		return false
	}
	if mode == bridgeconfig.WakeOwner {
		if !localOwnerMessage(message, networkID) {
			return false
		}
		if len(message.Mentions) == 0 {
			return true
		}
		return ShouldWakeForNetwork(bridgeconfig.WakeMentions, message.Target, message.Mentions, networkID, agent)
	}
	return ShouldWakeForNetwork(mode, message.Target, message.Mentions, networkID, agent)
}

func localOwnerMessage(message *protocol.Message, networkID string) bool {
	return networkID != "" && message.NetworkID == networkID && message.From.Type == "human" &&
		message.From.ID == "operator" &&
		(message.From.NetworkID == "" || message.From.NetworkID == networkID) &&
		(message.Origin.NetworkID == "" || message.Origin.NetworkID == networkID)
}
