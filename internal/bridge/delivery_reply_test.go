package bridge

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestDeliveryReplyMessageIDIsStableAndTargetScoped(t *testing.T) {
	room := protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}
	first := DeliveryReplyMessageID("brass", "moltnet:msg_1", room)
	if first != DeliveryReplyMessageID("brass", "moltnet:msg_1", room) {
		t.Fatal("delivery reply id is not stable")
	}
	for _, other := range []string{
		DeliveryReplyMessageID("other", "moltnet:msg_1", room),
		DeliveryReplyMessageID("brass", "moltnet:msg_2", room),
		DeliveryReplyMessageID("brass", "moltnet:msg_1", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}),
		DeliveryReplyMessageID("brass", "moltnet:msg_1", protocol.Target{Kind: protocol.TargetKindThread, RoomID: "research", ThreadID: "thread_1"}),
	} {
		if first == other {
			t.Fatalf("delivery reply id collision %q", first)
		}
	}
	if err := protocol.ValidateMessageID(first); err != nil {
		t.Fatalf("delivery reply id is not a valid message id: %v", err)
	}
}
