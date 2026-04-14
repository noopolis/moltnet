package loop

import (
	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func ShouldHandle(config bridgeconfig.Config, event protocol.Event) bool {
	if event.Type != protocol.EventTypeMessageCreated || event.Message == nil {
		return false
	}

	message := event.Message
	if message.NetworkID != config.Moltnet.NetworkID || protocol.ActorMatches(config.Moltnet.NetworkID, config.Agent.ID, message.From.ID) {
		return false
	}

	switch message.Target.Kind {
	case protocol.TargetKindRoom:
		return shouldHandleRoom(config, message)
	case protocol.TargetKindThread:
		return shouldHandleRoom(config, message)
	case protocol.TargetKindDM:
		return shouldHandleDirectMessage(config, message)
	default:
		return false
	}
}

func shouldHandleRoom(config bridgeconfig.Config, message *protocol.Message) bool {
	if message == nil {
		return false
	}

	for _, binding := range config.Rooms {
		if binding.ID == message.Target.RoomID {
			return bridgeutil.ShouldReadForNetwork(binding.Read, message.Target, message.Mentions, config.Moltnet.NetworkID, config.Agent) &&
				bridgeutil.ShouldReply(binding.Reply)
		}
	}

	return false
}

func shouldHandleDirectMessage(config bridgeconfig.Config, message *protocol.Message) bool {
	if config.DMs == nil || !config.DMs.Enabled || !bridgeutil.ShouldReadDirect(config.DMs.Read) || !bridgeutil.ShouldReply(config.DMs.Reply) {
		return false
	}

	for _, participantID := range message.Target.ParticipantIDs {
		if protocol.ActorMatches(config.Moltnet.NetworkID, config.Agent.ID, participantID) || participantID == config.Agent.Name {
			return true
		}
	}

	return false
}
