package loop

import (
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func conversationContextID(networkID string, message *protocol.Message) string {
	if message == nil {
		return ""
	}

	switch message.Target.Kind {
	case protocol.TargetKindRoom:
		if message.Target.RoomID != "" {
			return fmt.Sprintf("moltnet:%s:room:%s", networkID, message.Target.RoomID)
		}
	case protocol.TargetKindDM:
		if message.Target.DMID != "" {
			return fmt.Sprintf("moltnet:%s:dm:%s", networkID, message.Target.DMID)
		}
	case protocol.TargetKindThread:
		if message.Target.ThreadID != "" {
			return fmt.Sprintf("moltnet:%s:thread:%s", networkID, message.Target.ThreadID)
		}
	}

	if message.ID == "" {
		return ""
	}

	return fmt.Sprintf("moltnet:%s:%s", networkID, message.ID)
}
