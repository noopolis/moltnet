package store

import (
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMemoryStoreArtifactFiltersAndPagination(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	now := time.Now().UTC()

	roomMessage := protocol.Message{
		ID:        "msg_room",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "orchestrator"},
		Parts: []protocol.Part{
			{Kind: "text", Text: "hello"},
			{Kind: "file", URL: "https://example.com/spec.pdf", Filename: "spec.pdf", MediaType: "application/pdf"},
		},
		CreatedAt: now,
	}
	threadMessage := protocol.Message{
		ID:        "msg_thread",
		NetworkID: "local",
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_1",
			ParentMessageID: "msg_room",
		},
		From:      protocol.Actor{ID: "writer"},
		Parts:     []protocol.Part{{Kind: "image", URL: "https://example.com/mock.png", Filename: "mock.png", MediaType: "image/png"}},
		CreatedAt: now.Add(time.Second),
	}
	dmMessage := protocol.Message{
		ID:        "msg_dm",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"alpha", "beta"}},
		From:      protocol.Actor{ID: "alpha"},
		Parts:     []protocol.Part{{Kind: "audio", URL: "https://example.com/note.mp3", Filename: "note.mp3", MediaType: "audio/mpeg"}},
		CreatedAt: now.Add(2 * time.Second),
	}

	for _, message := range []protocol.Message{roomMessage, threadMessage, dmMessage} {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	allArtifacts, err := store.ListArtifacts(protocol.ArtifactFilter{}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts(all) error = %v", err)
	}
	if len(allArtifacts.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %#v", allArtifacts)
	}

	roomArtifacts, err := store.ListArtifacts(protocol.ArtifactFilter{RoomID: "research"}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts(room) error = %v", err)
	}
	if len(roomArtifacts.Artifacts) != 2 || roomArtifacts.Artifacts[0].MessageID != "msg_room" || roomArtifacts.Artifacts[1].MessageID != "msg_thread" {
		t.Fatalf("unexpected room artifacts %#v", roomArtifacts)
	}

	dmArtifacts, err := store.ListArtifacts(protocol.ArtifactFilter{DMID: "dm_1"}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts(dm) error = %v", err)
	}
	if len(dmArtifacts.Artifacts) != 1 || dmArtifacts.Artifacts[0].MessageID != "msg_dm" {
		t.Fatalf("unexpected dm artifacts %#v", dmArtifacts)
	}

	paged, err := store.ListArtifacts(protocol.ArtifactFilter{}, allArtifacts.Artifacts[2].ID, 1)
	if err != nil {
		t.Fatalf("ListArtifacts(paged) error = %v", err)
	}
	if len(paged.Artifacts) != 1 || paged.Artifacts[0].MessageID != "msg_thread" || !paged.Page.HasMore {
		t.Fatalf("unexpected paged artifacts %#v", paged)
	}
}

func TestArtifactHelpers(t *testing.T) {
	t.Parallel()

	if isArtifactPart(protocol.Part{Kind: "text", Text: "plain"}) {
		t.Fatal("plain text should not be treated as an artifact")
	}
	if !isArtifactPart(protocol.Part{Kind: "text", Filename: "note.txt"}) {
		t.Fatal("text with attachment metadata should be treated as an artifact")
	}

	artifacts := collectArtifacts([]protocol.Message{
		{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			Parts: []protocol.Part{
				{Kind: "text", Text: "hello"},
				{Kind: "json", MediaType: "application/json"},
			},
		},
	})
	if len(artifacts) != 1 || artifacts[0].ID != "art_msg_1_1" {
		t.Fatalf("unexpected collected artifacts %#v", artifacts)
	}

	page, err := pageArtifactsResult(artifacts, protocol.PageRequest{})
	if err != nil {
		t.Fatalf("pageArtifactsResult() error = %v", err)
	}
	if len(page.Artifacts) != 1 || page.Page.HasMore {
		t.Fatalf("unexpected artifact page %#v", page)
	}
}
