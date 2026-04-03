package tinyclaw

import (
	"testing"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func newTestBridge() *bridge {
	return &bridge{
		config: bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
			DMs: &bridgeconfig.DMConfig{
				Enabled: true,
				Read:    bridgeconfig.ReadAll,
			},
		},
		channel:   "moltnet",
		agentName: "Researcher",
		roomBindings: map[string]bridgeconfig.RoomBinding{
			"research": {ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto},
			"ops":      {ID: "ops", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto},
		},
	}
}

func TestShouldHandle(t *testing.T) {
	t.Parallel()

	bridge := newTestBridge()

	tests := []struct {
		name  string
		event protocol.Event
		want  bool
	}{
		{name: "missing message", event: protocol.Event{Type: protocol.EventTypeMessageCreated}, want: false},
		{
			name: "self message",
			event: protocol.Event{
				Type: protocol.EventTypeMessageCreated,
				Message: &protocol.Message{
					NetworkID: "local",
					From:      protocol.Actor{ID: "researcher"},
					Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "ops"},
				},
			},
			want: false,
		},
		{
			name: "mentioned room",
			event: protocol.Event{
				Type: protocol.EventTypeMessageCreated,
				Message: &protocol.Message{
					NetworkID: "local",
					From:      protocol.Actor{ID: "writer"},
					Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
					Mentions:  []string{"researcher"},
				},
			},
			want: true,
		},
		{
			name: "all room",
			event: protocol.Event{
				Type: protocol.EventTypeMessageCreated,
				Message: &protocol.Message{
					NetworkID: "local",
					From:      protocol.Actor{ID: "writer"},
					Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "ops"},
				},
			},
			want: true,
		},
		{
			name: "dm enabled",
			event: protocol.Event{
				Type: protocol.EventTypeMessageCreated,
				Message: &protocol.Message{
					NetworkID: "local",
					From:      protocol.Actor{ID: "writer"},
					Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1"},
				},
			},
			want: true,
		},
		{
			name: "wrong network",
			event: protocol.Event{
				Type: protocol.EventTypeMessageCreated,
				Message: &protocol.Message{
					NetworkID: "other",
					From:      protocol.Actor{ID: "writer"},
					Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "ops"},
				},
			},
			want: false,
		},
		{
			name: "missing mention in mention-only room",
			event: protocol.Event{
				Type: protocol.EventTypeMessageCreated,
				Message: &protocol.Message{
					NetworkID: "local",
					From:      protocol.Actor{ID: "writer"},
					Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
				},
			},
			want: false,
		},
		{
			name: "thread room",
			event: protocol.Event{
				Type: protocol.EventTypeMessageCreated,
				Message: &protocol.Message{
					NetworkID: "local",
					From:      protocol.Actor{ID: "writer"},
					Target:    protocol.Target{Kind: protocol.TargetKindThread, RoomID: "ops", ThreadID: "thr_1"},
				},
			},
			want: true,
		},
		{
			name: "unsupported target",
			event: protocol.Event{
				Type: protocol.EventTypeMessageCreated,
				Message: &protocol.Message{
					NetworkID: "local",
					From:      protocol.Actor{ID: "writer"},
					Target:    protocol.Target{Kind: "weird"},
				},
			},
			want: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := bridge.shouldHandle(test.event); got != test.want {
				t.Fatalf("shouldHandle() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestToTinyClawMessage(t *testing.T) {
	t.Parallel()

	bridge := newTestBridge()
	message, err := bridge.toTinyClawMessage(protocol.Event{
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			From:      protocol.Actor{ID: "writer", Name: "Writer"},
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			Parts: []protocol.Part{
				{Kind: "text", Text: "hello"},
				{Kind: "url", URL: "https://example.com"},
				{Kind: "data", Data: map[string]any{"files": []string{"note.txt"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("toTinyClawMessage() error = %v", err)
	}

	if message.SenderID != "room:research" || message.Channel != "moltnet" || message.MessageID != "msg_1" {
		t.Fatalf("unexpected message %#v", message)
	}
	if message.Sender != "Writer" {
		t.Fatalf("unexpected sender %q", message.Sender)
	}
	if message.Message == "" || message.Message[0] != '[' {
		t.Fatalf("unexpected rendered message %q", message.Message)
	}

	if _, err := bridge.toTinyClawMessage(protocol.Event{}); err == nil {
		t.Fatal("expected empty event error")
	}

	_, err = bridge.toTinyClawMessage(protocol.Event{
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			Target: protocol.Target{Kind: "weird"},
			Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
		},
	})
	if err == nil {
		t.Fatal("expected unsupported target error")
	}

	_, err = bridge.toTinyClawMessage(protocol.Event{
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		},
	})
	if err == nil {
		t.Fatal("expected missing text error")
	}
}

func TestToMoltnetMessageAndHelpers(t *testing.T) {
	t.Parallel()

	bridge := newTestBridge()

	request, err := bridge.toMoltnetMessage(tinyclawPendingResponse{
		ID:       pendingResponseID("7"),
		SenderID: "dm:dm_1",
		Message:  "done",
		Files:    []string{"report.md"},
	})
	if err != nil {
		t.Fatalf("toMoltnetMessage() error = %v", err)
	}
	if request.ID != "tinyclaw:researcher:7" {
		t.Fatalf("unexpected request id %q", request.ID)
	}
	if request.Target.Kind != protocol.TargetKindDM || request.Target.DMID != "dm_1" {
		t.Fatalf("unexpected target %#v", request.Target)
	}
	if len(request.Parts) != 2 {
		t.Fatalf("unexpected parts %#v", request.Parts)
	}

	if _, err := bridge.toMoltnetMessage(tinyclawPendingResponse{SenderID: "bad"}); err == nil {
		t.Fatal("expected bad target error")
	}
	if _, err := bridge.toMoltnetMessage(tinyclawPendingResponse{SenderID: "room:research"}); err == nil {
		t.Fatal("expected empty response error")
	}

	if !bridgeutil.ShouldRead(bridgeconfig.ReadMentions, protocol.Target{Kind: protocol.TargetKindRoom}, []string{"Researcher"}, bridge.config.Agent) {
		t.Fatal("expected mention read")
	}
	if bridgeutil.ShouldRead(bridgeconfig.ReadThreadOnly, protocol.Target{Kind: protocol.TargetKindRoom}, nil, bridge.config.Agent) {
		t.Fatal("did not expect thread_only read")
	}
	if !bridgeutil.ShouldRead(bridgeconfig.ReadThreadOnly, protocol.Target{Kind: protocol.TargetKindThread}, nil, bridge.config.Agent) {
		t.Fatal("expected thread_only to read threads")
	}
	if !bridgeutil.ShouldReadDirect(bridgeconfig.ReadMentions) {
		t.Fatal("expected direct mentions read")
	}
	if bridgeutil.ShouldReadDirect(bridgeconfig.ReadThreadOnly) {
		t.Fatal("did not expect direct thread_only read")
	}

	if actor := bridgeutil.SenderName(protocol.Actor{ID: "writer", Name: "Writer"}); actor != "Writer" {
		t.Fatalf("unexpected actor %q", actor)
	}
	if actor := bridgeutil.SenderName(protocol.Actor{ID: "writer"}); actor != "writer" {
		t.Fatalf("unexpected actor %q", actor)
	}

	if got := bridgeutil.TargetPrefix(protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "thr_1"}, "Writer"); got == "" {
		t.Fatal("expected thread prefix")
	}
	if got := bridgeutil.TargetPrefix(protocol.Target{Kind: "weird"}, "Writer"); got != "Writer" {
		t.Fatalf("unexpected default prefix %q", got)
	}

	if text, ok := bridgeutil.RenderDataPart(map[string]any{"files": []any{"a.txt", "b.txt"}}); !ok || text == "" {
		t.Fatal("expected rendered files payload")
	}
	if _, ok := bridgeutil.RenderDataPart(map[string]any{"ignored": true}); ok {
		t.Fatal("did not expect render for unrelated data")
	}

	if key, err := encodeTarget(protocol.Target{Kind: protocol.TargetKindThread, RoomID: "research", ThreadID: "thr_1", ParentMessageID: "msg_parent"}); err != nil || key != "thread:research:thr_1:msg_parent" {
		t.Fatalf("unexpected encoded target %q err=%v", key, err)
	}
	if _, err := encodeTarget(protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "thr_1"}); err == nil {
		t.Fatal("expected missing thread room error")
	}
	if _, err := encodeTarget(protocol.Target{Kind: "weird"}); err == nil {
		t.Fatal("expected unsupported target error")
	}
	if decoded, err := decodeTarget("room:research"); err != nil || decoded.RoomID != "research" {
		t.Fatalf("unexpected decode %#v err=%v", decoded, err)
	}
	if _, err := decodeTarget("bad"); err == nil {
		t.Fatal("expected invalid target error")
	}
	if _, err := decodeTarget("weird:value"); err == nil {
		t.Fatal("expected invalid target kind error")
	}
	if decoded, err := decodeTarget("thread:research:thr_1:msg_parent"); err != nil || decoded.RoomID != "research" || decoded.ThreadID != "thr_1" || decoded.ParentMessageID != "msg_parent" {
		t.Fatalf("unexpected thread decode %#v err=%v", decoded, err)
	}

	if rendered := bridgeutil.RenderInboundText(&protocol.Message{
		From:   protocol.Actor{ID: "writer"},
		Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hi"}},
	}); rendered == "" {
		t.Fatal("expected rendered inbound text")
	}
	if rendered := bridgeutil.RenderInboundText(nil); rendered != "" {
		t.Fatalf("expected empty render, got %q", rendered)
	}
}
