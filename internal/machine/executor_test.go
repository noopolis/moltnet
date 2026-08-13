package machine

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestDeliveryIdentityFromSendNudge(t *testing.T) {
	t.Run("identityAndFingerprint", func(t *testing.T) {
		resolver := DeliveryIdentityFromSendNudge{}
		request := protocol.MachineRequest{
			Version:       protocol.MachineProtocolV1,
			CorrelationID: "corr_1",
			Operation:     protocol.MachineOpSendNudge,
			SendNudge: &protocol.MachineSendNudgeRequest{
				DeliveryID:      "delivery_1",
				Target:          protocol.MachineTarget{Kind: protocol.MachineTargetKindRoom, ID: "room_1"},
				Body:            "hello",
				OriginMessageID: "msg_1",
				CauseEventIDs:   []string{"c1", "c2"},
			},
		}

		identity, ok := resolver.Identity(request)
		if !ok {
			t.Fatal("expected send-nudge identity")
		}
		if identity.Identity != request.SendNudge.DeliveryID {
			t.Fatalf("wrong identity: %q", identity.Identity)
		}
		if identity.Fingerprint != canonicalSendNudgeFingerprint(request.Operation, request.SendNudge) {
			t.Fatalf("unexpected fingerprint")
		}
	})

	t.Run("ignoresCorrelation", func(t *testing.T) {
		resolver := DeliveryIdentityFromSendNudge{}
		requestA := protocol.MachineRequest{
			Version:       protocol.MachineProtocolV1,
			CorrelationID: "corr_a",
			Operation:     protocol.MachineOpSendNudge,
			SendNudge: &protocol.MachineSendNudgeRequest{
				DeliveryID: "delivery_1",
				Target:     protocol.MachineTarget{Kind: protocol.MachineTargetKindRoom, ID: "room_1"},
				Body:       "same-body",
			},
		}
		requestB := requestA
		requestB.CorrelationID = "corr_b"

		identityA, okA := resolver.Identity(requestA)
		identityB, okB := resolver.Identity(requestB)
		if !okA || !okB || identityA.Identity != identityB.Identity || identityA.Fingerprint != identityB.Fingerprint {
			t.Fatalf("expected identical identity across correlation changes")
		}
	})
}
