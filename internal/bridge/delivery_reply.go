package bridge

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const DaimonWakeIDEnv = "DAIMON_WAKE_ID"

// DeliveryReplyMessageID reserves one idempotent publication slot for an
// attached agent's reply to one target during one delivered wake.
func DeliveryReplyMessageID(agentID, deliveryID string, target protocol.Target) string {
	sum := sha256.Sum256([]byte("moltnet.delivery-reply.v1\x00" + agentID + "\x00" + deliveryID + "\x00" + replyTargetKey(target)))
	return "delivery_reply_" + hex.EncodeToString(sum[:16])
}

func replyTargetKey(target protocol.Target) string {
	switch target.Kind {
	case protocol.TargetKindRoom:
		return target.Kind + ":" + target.RoomID
	case protocol.TargetKindThread:
		return target.Kind + ":" + target.RoomID + ":" + target.ThreadID
	case protocol.TargetKindDM:
		return target.Kind + ":" + target.DMID
	default:
		return target.Kind
	}
}
