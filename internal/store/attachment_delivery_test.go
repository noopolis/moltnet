package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestFileStorePersistsAttachmentBaselineReplayAndACK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moltnet.json")
	first, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := first.PrepareAttachmentDeliveryContext(t.Context(), "alpha"); err != nil || len(replay) != 0 {
		t.Fatalf("first baseline = %#v, %v", replay, err)
	}
	event := fileDeliveryEvent("msg_1")
	if _, err := first.AppendMessageEventWithLifecycleContext(t.Context(), *event.Message, event); err != nil {
		t.Fatalf("append event: %v", err)
	}

	second, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := second.PrepareAttachmentDeliveryContext(t.Context(), "alpha")
	if err != nil || len(replay) != 1 || replay[0].ID != event.ID {
		t.Fatalf("restart replay = %#v, %v", replay, err)
	}
	if err := second.AcknowledgeAttachmentDeliveryContext(t.Context(), "alpha", event.ID); err != nil {
		t.Fatalf("ack event: %v", err)
	}

	third, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := third.PrepareAttachmentDeliveryContext(t.Context(), "alpha"); err != nil || len(replay) != 0 {
		t.Fatalf("acked restart replay = %#v, %v", replay, err)
	}
	if replay, err := third.PrepareAttachmentDeliveryContext(t.Context(), "beta"); err != nil || len(replay) != 0 {
		t.Fatalf("new beta baseline = %#v, %v", replay, err)
	}
}

func fileDeliveryEvent(messageID string) protocol.Event {
	createdAt := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	message := protocol.Message{
		ID: messageID, NetworkID: "local", From: protocol.Actor{Type: "agent", ID: "writer"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}}, CreatedAt: createdAt,
	}
	return protocol.Event{
		ID: protocol.MessageEventID(messageID), Type: protocol.EventTypeMessageCreated,
		NetworkID: "local", Message: &message, CreatedAt: createdAt,
	}
}
