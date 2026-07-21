package bridge

import (
	"strings"
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

	body := RenderMessageBody(message)
	expectedBody := "hello\nhttps://example.com/report\nfiles: report.md, summary.txt"
	if body != expectedBody {
		t.Fatalf("unexpected rendered body %q", body)
	}

	compact := RenderCompactInboundMessage("local_lab", &protocol.Message{
		ID:       "msg_42",
		Target:   protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:     protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
		Mentions: []string{"reviewer"},
		Parts:    []protocol.Part{{Kind: "text", Text: "hello"}},
	}, true)
	expectedCompact := strings.Join([]string{
		"Channel: moltnet",
		"Chat ID: local_lab:room:research",
		"From: local_lab/agent/writer",
		"Name: Writer",
		"Mentions: reviewer",
		"Message ID: msg_42",
		"",
		"Message:",
		"hello",
	}, "\n")
	if compact != expectedCompact {
		t.Fatalf("unexpected compact message %q", compact)
	}

	bootstrap := RenderCompactBootstrapMessage("local_lab", protocol.Target{
		Kind:   protocol.TargetKindRoom,
		RoomID: "research",
	}, true)
	expectedBootstrap := strings.Join([]string{
		"Channel: moltnet",
		"Chat ID: local_lab:room:research",
		"",
		"Moltnet conversation attached. Use the `moltnet` skill in this conversation.",
	}, "\n")
	if bootstrap != expectedBootstrap {
		t.Fatalf("unexpected bootstrap message %q", bootstrap)
	}
}

func TestBridgeHelpers(t *testing.T) {
	t.Parallel()

	agent := bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"}
	if !ShouldWake("", protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected default wake mode to wake")
	}
	if !ShouldWake(bridgeconfig.WakeMentions, protocol.Target{Kind: protocol.TargetKindRoom}, []string{"Researcher"}, agent) {
		t.Fatal("expected mention wake")
	}
	if !ShouldWakeForNetwork(
		bridgeconfig.WakeMentions,
		protocol.Target{Kind: protocol.TargetKindRoom},
		[]string{protocol.AgentFQID("local", "researcher")},
		"local",
		agent,
	) {
		t.Fatal("expected canonical mention wake")
	}
	if ShouldWakeForNetwork(
		bridgeconfig.WakeMentions,
		protocol.Target{Kind: protocol.TargetKindRoom},
		[]string{protocol.AgentFQID("remote", "researcher")},
		"local",
		agent,
	) {
		t.Fatal("expected remote canonical mention to be ignored")
	}
	if ShouldWake(bridgeconfig.WakeMentions, protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected missing mention to be ignored")
	}
	if ShouldWake(bridgeconfig.WakeThreadOnly, protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected thread-only mode to ignore room messages")
	}
	if !ShouldWake(bridgeconfig.WakeThreadOnly, protocol.Target{Kind: protocol.TargetKindThread}, nil, agent) {
		t.Fatal("expected thread-only mode to wake on thread targets")
	}
	if ShouldWake(bridgeconfig.WakeNever, protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected never mode to be ignored")
	}
	if ShouldWake(bridgeconfig.WakeConfig("invalid"), protocol.Target{Kind: protocol.TargetKindRoom}, nil, agent) {
		t.Fatal("expected invalid wake mode to be ignored")
	}
	if !ShouldWakeDirect(bridgeconfig.WakeMentions) {
		t.Fatal("expected direct mentions mode to wake")
	}
	if ShouldWakeDirect(bridgeconfig.WakeThreadOnly) {
		t.Fatal("expected thread-only direct mode to be ignored")
	}
	if !ShouldBootstrap(bridgeconfig.WakeAll) {
		t.Fatal("expected all wake mode to bootstrap")
	}
	if ShouldBootstrap(bridgeconfig.WakeMentions) || ShouldBootstrap(bridgeconfig.WakeNever) {
		t.Fatal("expected mention and never wake modes to skip bootstrap")
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
	if ChatID("local_lab", protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}) != "local_lab:room:research" {
		t.Fatal("expected room chat id")
	}
	if ChatID("local_lab", protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_writer_reviewer"}) != "local_lab:dm:dm_writer_reviewer" {
		t.Fatal("expected dm chat id")
	}
	if ActorAddress("local_lab", protocol.Actor{Type: "agent", ID: "writer"}) != "local_lab/agent/writer" {
		t.Fatal("expected actor address")
	}
	if ActorAddress("local_lab", protocol.Actor{FQID: "remote/agent/writer"}) != "remote/agent/writer" {
		t.Fatal("expected actor address to prefer fqid")
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

func TestShouldWakeDirectMessageAllowedSenders(t *testing.T) {
	t.Parallel()

	base := protocol.Message{
		NetworkID: "pitch",
		From: protocol.Actor{
			Type:            "service",
			ID:              "world",
			NetworkID:       "pitch",
			FQID:            protocol.AgentFQID("pitch", "world"),
			CredentialBound: true,
		},
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "world-red",
			ParticipantIDs: []string{"world", "red"},
		},
	}
	agent := bridgeconfig.AgentConfig{ID: "red", Name: "Red"}
	strict := &bridgeconfig.DMConfig{
		Enabled:            true,
		Wake:               bridgeconfig.WakeAll,
		AllowedWakeSenders: []string{"world"},
	}
	social := &bridgeconfig.DMConfig{Enabled: true, Wake: bridgeconfig.WakeAll}
	withoutProvenance := base
	withoutProvenance.From.CredentialBound = false
	if !ShouldWakeDirectMessage(social, "pitch", agent, &withoutProvenance) {
		t.Fatal("empty allowlist should preserve ordinary social DM wake behavior")
	}

	tests := []struct {
		name    string
		config  *bridgeconfig.DMConfig
		agent   bridgeconfig.AgentConfig
		message protocol.Message
		want    bool
	}{
		{name: "exact pair", config: strict, agent: agent, message: base, want: true},
		{
			name:   "network-qualified exact pair",
			config: strict,
			agent:  agent,
			message: func() protocol.Message {
				message := base
				message.Target.ParticipantIDs = []string{
					protocol.ScopedAgentID("pitch", "world"),
					protocol.AgentFQID("pitch", "red"),
				}
				return message
			}(),
			want: true,
		},
		{name: "unbound sender", config: strict, agent: agent, message: withoutProvenance},
		{
			name:   "other sender",
			config: strict,
			agent:  agent,
			message: func() protocol.Message {
				message := base
				message.From.ID = "coach"
				message.From.FQID = protocol.AgentFQID("pitch", "coach")
				message.Target.ParticipantIDs = []string{"coach", "red"}
				return message
			}(),
		},
		{
			name:   "third participant",
			config: strict,
			agent:  agent,
			message: func() protocol.Message {
				message := base
				message.Target.ParticipantIDs = []string{"world", "red", "blue"}
				return message
			}(),
		},
		{
			name:   "other direct conversation",
			config: strict,
			agent:  agent,
			message: func() protocol.Message {
				message := base
				message.Target.ParticipantIDs = []string{"world", "blue"}
				return message
			}(),
		},
		{name: "opposite attachment", config: strict, agent: bridgeconfig.AgentConfig{ID: "world"}, message: base},
		{
			name:   "remote sender collision",
			config: strict,
			agent:  agent,
			message: func() protocol.Message {
				message := base
				message.From.NetworkID = "remote"
				message.From.FQID = protocol.AgentFQID("remote", "world")
				return message
			}(),
		},
		{
			name:   "remote participant collision",
			config: strict,
			agent:  agent,
			message: func() protocol.Message {
				message := base
				message.Target.ParticipantIDs = []string{"remote:world", "pitch:red"}
				return message
			}(),
		},
		{
			name:   "duplicate identity aliases",
			config: strict,
			agent:  agent,
			message: func() protocol.Message {
				message := base
				message.Target.ParticipantIDs = []string{"world", "pitch:world"}
				return message
			}(),
		},
		{
			name:    "disabled",
			config:  &bridgeconfig.DMConfig{AllowedWakeSenders: []string{"world"}},
			agent:   agent,
			message: base,
		},
		{
			name:    "wake never",
			config:  &bridgeconfig.DMConfig{Enabled: true, Wake: bridgeconfig.WakeNever, AllowedWakeSenders: []string{"world"}},
			agent:   agent,
			message: base,
		},
		{
			name:   "shared room is not a direct wake",
			config: strict,
			agent:  agent,
			message: func() protocol.Message {
				message := base
				message.Target = protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "shared"}
				return message
			}(),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldWakeDirectMessage(test.config, "pitch", test.agent, &test.message); got != test.want {
				t.Fatalf("ShouldWakeDirectMessage() = %v, want %v", got, test.want)
			}
		})
	}
}
