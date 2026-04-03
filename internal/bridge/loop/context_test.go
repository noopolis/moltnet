package loop

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestConversationContextID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		message *protocol.Message
		name    string
		want    string
	}{
		{
			name: "room target",
			message: &protocol.Message{
				ID:     "msg_1",
				Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			},
			want: "moltnet:local_lab:room:research",
		},
		{
			name: "dm target",
			message: &protocol.Message{
				ID:     "msg_2",
				Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-orchestrator-researcher"},
			},
			want: "moltnet:local_lab:dm:dm-orchestrator-researcher",
		},
		{
			name: "thread target",
			message: &protocol.Message{
				ID:     "msg_3",
				Target: protocol.Target{Kind: protocol.TargetKindThread, ThreadID: "handoff"},
			},
			want: "moltnet:local_lab:thread:handoff",
		},
		{
			name: "fallback to message id",
			message: &protocol.Message{
				ID:     "msg_4",
				Target: protocol.Target{Kind: "unknown"},
			},
			want: "moltnet:local_lab:msg_4",
		},
		{
			name: "nil message",
			want: "",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := conversationContextID("local_lab", test.message); got != test.want {
				t.Fatalf("conversationContextID() = %q, want %q", got, test.want)
			}
		})
	}
}
