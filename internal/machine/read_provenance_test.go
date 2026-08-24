package machine

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// The machine JSONL contract is a frozen, strictly-validated v1 wire: a
// conforming consumer rejects fields it does not know. Origin.ReceivedVia is
// this network's own operator-facing bookkeeping about which pairing carried a
// message, so it must never reach that wire -- adding it would silently break
// every strict v1 consumer the moment a relayed message appeared.
//
// cloneReadMessage rebuilds the origin field by field precisely so new fields
// are excluded by default rather than included by default. This pins that.
func TestMachineReadWireExcludesPairingProvenance(t *testing.T) {
	stored := protocol.Message{
		ID:        "msg_1",
		NetworkID: "local",
		Origin: protocol.MessageOrigin{
			NetworkID:   "remote-b",
			MessageID:   "remote-b-1",
			ReceivedVia: "bob-invite",
		},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "floor"},
		From:   protocol.Actor{Type: "agent", ID: "researcher", NetworkID: "remote-b"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hi"}},
	}
	copied, ok := cloneReadMessage(stored)
	if !ok {
		t.Fatal("cloneReadMessage() refused a well-formed message")
	}
	if got := protocol.Message(copied).Origin.ReceivedVia; got != "" {
		t.Fatalf("machine read wire leaked pairing provenance: ReceivedVia = %q", got)
	}
	if protocol.Message(copied).Origin.NetworkID != "remote-b" {
		t.Fatal("excluding provenance must not disturb the rest of the origin")
	}
}
