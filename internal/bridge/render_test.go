package bridge

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRenderInboundTextAndMentions(t *testing.T) {
	t.Parallel()

	message := &protocol.Message{
		Target: protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "thread_1"},
		From:   protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
		Parts: []protocol.Part{
			{Kind: "text", Text: "hello"},
			{Kind: "url", URL: "https://example.com/report"},
			{Kind: "data", Data: map[string]any{"files": []any{"report.md", "summary.txt"}}},
		},
	}

	rendered := RenderInboundText(message)
	expected := "[thread thread_1] Writer\nhello\nhttps://example.com/report\nfiles: report.md, summary.txt"
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

	mentions := ParseMentions("@writer please ask @reviewer and @writer again")
	if len(mentions) != 2 || mentions[0] != "writer" || mentions[1] != "reviewer" {
		t.Fatalf("unexpected mentions %#v", mentions)
	}
	if mentions := ParseMentions("no mentions here"); mentions != nil {
		t.Fatalf("expected nil mentions, got %#v", mentions)
	}
}

func TestBridgeHelpers(t *testing.T) {
	t.Parallel()

	agent := bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"}
	if !ShouldRead("", protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected default read mode to read")
	}
	if !ShouldRead(bridgeconfig.ReadMentions, protocol.Target{Kind: protocol.TargetKindRoom}, []string{"Researcher"}, agent) {
		t.Fatal("expected mention read")
	}
	if ShouldRead(bridgeconfig.ReadMentions, protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected missing mention to be ignored")
	}
	if ShouldRead(bridgeconfig.ReadThreadOnly, protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected thread-only mode to ignore room messages")
	}
	if !ShouldRead(bridgeconfig.ReadThreadOnly, protocol.Target{Kind: protocol.TargetKindThread}, nil, agent) {
		t.Fatal("expected thread-only mode to read thread targets")
	}
	if ShouldRead(bridgeconfig.ReadConfig("invalid"), protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected invalid read mode to be ignored")
	}
	if !ShouldReadDirect(bridgeconfig.ReadMentions) {
		t.Fatal("expected direct mentions mode to be readable")
	}
	if ShouldReadDirect(bridgeconfig.ReadThreadOnly) {
		t.Fatal("expected thread-only direct mode to be ignored")
	}
	if !ShouldReply(bridgeconfig.ReplyAuto) {
		t.Fatal("expected auto reply mode")
	}
	if ShouldReply(bridgeconfig.ReplyNever) {
		t.Fatal("expected never reply mode to disable auto replies")
	}
	if SenderName(protocol.Actor{ID: "writer", Name: "Writer"}) != "Writer" {
		t.Fatal("expected sender name to prefer actor name")
	}
	if SenderName(protocol.Actor{ID: "writer"}) != "writer" {
		t.Fatal("expected sender name fallback to id")
	}
	if DisplayName(bridgeconfig.AgentConfig{ID: "researcher"}) != "researcher" {
		t.Fatal("expected display name fallback to id")
	}
	if DisplayName(agent) != "Researcher" {
		t.Fatal("expected display name to prefer agent name")
	}
	if TargetPrefix(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}, "Writer") != "[room research] Writer" {
		t.Fatal("expected room target prefix")
	}
	if TargetPrefix(protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1"}, "Writer") != "[dm] Writer" {
		t.Fatal("expected dm target prefix")
	}
	if TargetPrefix(protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "thread_1"}, "Writer") != "[thread thread_1] Writer" {
		t.Fatal("expected thread target prefix")
	}
	if TargetPrefix(protocol.Target{Kind: "unknown"}, "Writer") != "Writer" {
		t.Fatal("expected unknown target prefix fallback")
	}
	if payload, ok := RenderDataPart(map[string]any{"files": []any{1, true}}); ok || payload != "" {
		t.Fatal("expected invalid file payload to be ignored")
	}
	if payload, ok := RenderDataPart(map[string]any{"files": []string{"one.txt"}}); !ok || payload != "files: one.txt" {
		t.Fatalf("unexpected rendered payload %q %v", payload, ok)
	}
	if payload, ok := RenderDataPart(map[string]any{"ignored": true}); ok || payload != "" {
		t.Fatal("expected unrelated data to be ignored")
	}
}
