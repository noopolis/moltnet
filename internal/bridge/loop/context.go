package loop

import (
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

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
