package loop

import (
	"testing"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestShouldHandle(t *testing.T) {
	t.Parallel()

	event := protocol.Event{
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Mentions:  []string{"researcher"},
			CreatedAt: time.Now().UTC(),
		},
	}

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{
			NetworkID: "local",
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto},
		},
	}

	if !ShouldHandle(config, event) {
		t.Fatal("expected room mention to be handled")
	}

	event.Message.From.ID = "researcher"
	if ShouldHandle(config, event) {
		t.Fatal("expected self-authored message to be ignored")
	}

	event.Message.From.ID = "writer"
	event.Message.Mentions = nil
	if ShouldHandle(config, event) {
		t.Fatal("expected mention-gated room message to be ignored")
	}

	event.Message.Target = protocol.Target{Kind: protocol.TargetKindThread, RoomID: "research", ThreadID: "thread_1"}
	event.Message.Mentions = []string{"researcher"}
	if !ShouldHandle(config, event) {
		t.Fatal("expected thread mention to be handled")
	}

	event.Message.Target = protocol.Target{
		Kind:           protocol.TargetKindDM,
		DMID:           "dm_1",
		ParticipantIDs: []string{"researcher", "writer"},
	}
	config.DMs = &bridgeconfig.DMConfig{Enabled: true, Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto}
	if !ShouldHandle(config, event) {
		t.Fatal("expected dm to be handled")
	}

	event.Message.Target.ParticipantIDs = []string{"writer", "orchestrator"}
	if ShouldHandle(config, event) {
		t.Fatal("expected unrelated dm to be ignored")
	}

	config.Moltnet.NetworkID = "net_b"
	event.Message.NetworkID = "net_b"
	event.Message.Target.ParticipantIDs = []string{"net_a:writer", "net_b:researcher"}
	if !ShouldHandle(config, event) {
		t.Fatal("expected scoped dm to be handled")
	}
	config.Moltnet.NetworkID = "local"
	event.Message.NetworkID = "local"

	config.DMs.Reply = bridgeconfig.ReplyManual
	event.Message.Target.ParticipantIDs = []string{"researcher", "writer"}
	if ShouldHandle(config, event) {
		t.Fatal("expected manual reply policy to skip auto handling")
	}

	if ShouldHandle(config, protocol.Event{Type: "other"}) {
		t.Fatal("expected non-message event to be ignored")
	}

	event.Message.NetworkID = "other"
	if ShouldHandle(config, event) {
		t.Fatal("expected other network to be ignored")
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Parallel()

	if !shouldHandleDirectMessage(
		bridgeconfig.Config{
			Agent: bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{
				NetworkID: "net_b",
			},
			DMs: &bridgeconfig.DMConfig{Enabled: true, Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto},
		},
		&protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"net_a:writer", "net_b:researcher"}}},
	) {
		t.Fatal("expected matching scoped participant dm to be handled")
	}
	if shouldHandleDirectMessage(
		bridgeconfig.Config{
			Agent: bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{
				NetworkID: "net_b",
			},
			DMs: &bridgeconfig.DMConfig{Enabled: true, Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto},
		},
		&protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"net_a:writer", "net_c:orchestrator"}}},
	) {
		t.Fatal("expected non-participant dm to be ignored")
	}
	if bridgeutil.TargetPrefix(protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "thread_1"}, "Writer") != "[thread thread_1] Writer" {
		t.Fatal("expected thread target prefix")
	}
	if bridgeutil.TargetPrefix(protocol.Target{Kind: "unknown"}, "Writer") != "Writer" {
		t.Fatal("expected unknown target prefix fallback")
	}
	if payload, ok := bridgeutil.RenderDataPart(map[string]any{"files": []any{1, true}}); ok || payload != "" {
		t.Fatal("expected invalid file payload to be ignored")
	}
	if mentions := bridgeutil.ParseMentions("no mentions here"); mentions != nil {
		t.Fatalf("expected nil mentions, got %#v", mentions)
	}
}
