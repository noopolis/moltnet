package openclaw

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestShouldDeliverDistinguishesRemoteSenderWithSameBareID(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
		Rooms:   []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeAll}},
	}
	event := protocol.Event{
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From: protocol.Actor{
				ID:        "reviewer",
				NetworkID: "remote",
				FQID:      protocol.AgentFQID("remote", "reviewer"),
			},
		},
	}
	if !shouldDeliver(config, event) {
		t.Fatal("expected remote sender with the same bare id not to be suppressed as self-authored")
	}

	event.Message.From = protocol.Actor{ID: "reviewer"}
	if shouldDeliver(config, event) {
		t.Fatal("expected local self-authored message to be suppressed")
	}
}
