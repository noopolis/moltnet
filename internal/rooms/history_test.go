package rooms

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestServiceThreadAndArtifactErrors(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.ListThreads("missing"); err == nil {
		t.Fatal("expected missing room error")
	}
	if _, err := service.ListThreadMessages("missing", "", 10); err == nil {
		t.Fatal("expected missing thread error")
	}
	if _, err := service.ListArtifacts(protocol.ArtifactFilter{RoomID: "missing"}, "", 10); err == nil {
		t.Fatal("expected missing room artifact error")
	}
	if _, err := service.ListArtifacts(protocol.ArtifactFilter{ThreadID: "missing"}, "", 10); err == nil {
		t.Fatal("expected missing thread artifact error")
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "missing",
			ThreadID:        "thread_1",
			ParentMessageID: "msg_parent",
		},
		From:  protocol.Actor{Type: "agent", ID: "writer"},
		Parts: []protocol.Part{{Kind: "text", Text: "reply"}},
	}); err == nil {
		t.Fatal("expected missing thread room error")
	}
}
