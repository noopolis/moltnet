package loop

import (
	"testing"
	"time"

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

func TestRenderInboundTextAndMentions(t *testing.T) {
	t.Parallel()

	message := &protocol.Message{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
		Parts: []protocol.Part{
			{Kind: "text", Text: "hello"},
			{Kind: "url", URL: "https://example.com/report"},
			{Kind: "data", Data: map[string]any{"files": []any{"report.md", "summary.txt"}}},
		},
	}

	rendered := RenderInboundText(message)
	expected := "[room research] Writer\nhello\nhttps://example.com/report\nfiles: report.md, summary.txt"
	if rendered != expected {
		t.Fatalf("unexpected rendered text %q", rendered)
	}

	if RenderInboundText(&protocol.Message{
		Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1"},
		From:   protocol.Actor{Type: "human", ID: "apresmoi"},
		Parts:  []protocol.Part{{Kind: "data", Data: map[string]any{"files": []any{true}}}},
	}) != "" {
		t.Fatal("expected unsupported parts to render empty text")
	}

	mentions := parseMentions("@writer please ask @reviewer and @writer again")
	if len(mentions) != 2 || mentions[0] != "writer" || mentions[1] != "reviewer" {
		t.Fatalf("unexpected mentions %#v", mentions)
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Parallel()

	if !shouldRead("", nil, bridgeconfig.AgentConfig{ID: "researcher"}) {
		t.Fatal("expected default read mode to read")
	}
	if shouldRead(bridgeconfig.ReadThreadOnly, []string{"researcher"}, bridgeconfig.AgentConfig{ID: "researcher"}) {
		t.Fatal("expected thread-only mode to be skipped by the generic room reader")
	}
	if !shouldReadDirect(bridgeconfig.ReadMentions) {
		t.Fatal("expected direct mentions mode to be readable")
	}
	if shouldReadDirect(bridgeconfig.ReadThreadOnly) {
		t.Fatal("expected thread-only direct mode to be ignored")
	}
	if !shouldHandleDirectMessage(
		bridgeconfig.Config{
			Agent: bridgeconfig.AgentConfig{ID: "researcher"},
			DMs:   &bridgeconfig.DMConfig{Enabled: true, Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto},
		},
		&protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"researcher", "writer"}}},
	) {
		t.Fatal("expected matching participant dm to be handled")
	}
	if shouldHandleDirectMessage(
		bridgeconfig.Config{
			Agent: bridgeconfig.AgentConfig{ID: "researcher"},
			DMs:   &bridgeconfig.DMConfig{Enabled: true, Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto},
		},
		&protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"writer", "orchestrator"}}},
	) {
		t.Fatal("expected non-participant dm to be ignored")
	}
	if shouldReply(bridgeconfig.ReplyNever) {
		t.Fatal("expected reply never to disable automatic replies")
	}
	if senderName(protocol.Actor{ID: "writer"}) != "writer" {
		t.Fatal("expected sender fallback to actor id")
	}
	if displayName(bridgeconfig.AgentConfig{ID: "researcher"}) != "researcher" {
		t.Fatal("expected display name fallback to agent id")
	}
	if targetPrefix(protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "thread_1"}, "Writer") != "[thread thread_1] Writer" {
		t.Fatal("expected thread target prefix")
	}
	if targetPrefix(protocol.Target{Kind: "unknown"}, "Writer") != "Writer" {
		t.Fatal("expected unknown target prefix fallback")
	}
	if payload, ok := renderDataPart(map[string]any{"files": []any{1, true}}); ok || payload != "" {
		t.Fatal("expected invalid file payload to be ignored")
	}
	if mentions := parseMentions("no mentions here"); mentions != nil {
		t.Fatalf("expected nil mentions, got %#v", mentions)
	}
}
