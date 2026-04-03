package store

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPostgresStoreIntegration(t *testing.T) {
	dsn := os.Getenv("MOLTNET_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOLTNET_TEST_POSTGRES_DSN is not set")
	}

	store, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	defer store.Close()

	if _, err := store.db.Exec(`DELETE FROM artifacts; DELETE FROM dm_participants; DELETE FROM dm_conversations; DELETE FROM messages; DELETE FROM threads; DELETE FROM room_members; DELETE FROM rooms;`); err != nil {
		t.Fatalf("reset postgres tables: %v", err)
	}

	room := protocol.Room{
		ID:        "research",
		NetworkID: "remote",
		FQID:      protocol.RoomFQID("remote", "research"),
		Name:      "Research",
		Members:   []string{"alpha", "beta"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	if err := store.AppendMessage(protocol.Message{
		ID:        "msg_pg",
		NetworkID: "remote",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:     []protocol.Part{{Kind: "text", Text: "hello postgres"}},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	page, err := store.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "msg_pg" {
		t.Fatalf("unexpected postgres page %#v", page)
	}
}

func TestPostgresStorePaginationThreadsDMsArtifactsAndConflicts(t *testing.T) {
	dsn := os.Getenv("MOLTNET_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOLTNET_TEST_POSTGRES_DSN is not set")
	}

	store, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	defer store.Close()

	if _, err := store.db.Exec(`DELETE FROM artifacts; DELETE FROM dm_participants; DELETE FROM dm_conversations; DELETE FROM messages; DELETE FROM threads; DELETE FROM room_members; DELETE FROM rooms;`); err != nil {
		t.Fatalf("reset postgres tables: %v", err)
	}

	room := protocol.Room{
		ID:        "research",
		NetworkID: "remote",
		FQID:      protocol.RoomFQID("remote", "research"),
		Name:      "Research",
		Members:   []string{"alpha", "beta"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	if err := store.CreateRoom(room); !errors.Is(err, ErrRoomExists) {
		t.Fatalf("expected ErrRoomExists, got %v", err)
	}

	now := time.Now().UTC()
	for _, message := range []protocol.Message{
		{
			ID:        "msg_room_1",
			NetworkID: "remote",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "alpha"},
			Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: "one"}},
			CreatedAt: now,
		},
		{
			ID:        "msg_room_2",
			NetworkID: "remote",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "beta"},
			Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: "two"}},
			CreatedAt: now.Add(time.Second),
		},
		{
			ID:        "msg_thread_1",
			NetworkID: "remote",
			Target: protocol.Target{
				Kind:            protocol.TargetKindThread,
				RoomID:          "research",
				ThreadID:        "thread_1",
				ParentMessageID: "msg_room_1",
			},
			From:      protocol.Actor{Type: "agent", ID: "beta"},
			Parts:     []protocol.Part{{Kind: protocol.PartKindImage, URL: "https://example.com/mock.png", Filename: "mock.png", MediaType: "image/png"}},
			CreatedAt: now.Add(2 * time.Second),
		},
		{
			ID:        "msg_dm_1",
			NetworkID: "remote",
			Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_pg", ParticipantIDs: []string{"alpha", "beta"}},
			From:      protocol.Actor{Type: "agent", ID: "alpha"},
			Parts:     []protocol.Part{{Kind: protocol.PartKindAudio, URL: "https://example.com/note.mp3", Filename: "note.mp3", MediaType: "audio/mpeg"}},
			CreatedAt: now.Add(3 * time.Second),
		},
	} {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage(%s) error = %v", message.ID, err)
		}
	}

	firstPage, err := store.ListRoomMessages("research", "", 1)
	if err != nil {
		t.Fatalf("ListRoomMessages(first) error = %v", err)
	}
	if len(firstPage.Messages) != 1 || firstPage.Messages[0].ID != "msg_room_2" || !firstPage.Page.HasMore {
		t.Fatalf("unexpected room first page %#v", firstPage)
	}
	secondPage, err := store.ListRoomMessages("research", firstPage.Page.NextBefore, 1)
	if err != nil {
		t.Fatalf("ListRoomMessages(second) error = %v", err)
	}
	if len(secondPage.Messages) != 1 || secondPage.Messages[0].ID != "msg_room_1" || secondPage.Page.HasMore {
		t.Fatalf("unexpected room second page %#v", secondPage)
	}
	if _, err := store.ListRoomMessages("research", "missing", 1); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor for missing cursor, got %v", err)
	}

	threads, err := store.ListThreads("research")
	if err != nil {
		t.Fatalf("ListThreads() error = %v", err)
	}
	if len(threads) != 1 || threads[0].ID != "thread_1" || threads[0].MessageCount != 1 {
		t.Fatalf("unexpected threads %#v", threads)
	}
	threadPage, err := store.ListThreadMessages("thread_1", "", 10)
	if err != nil {
		t.Fatalf("ListThreadMessages() error = %v", err)
	}
	if len(threadPage.Messages) != 1 || threadPage.Messages[0].ID != "msg_thread_1" {
		t.Fatalf("unexpected thread page %#v", threadPage)
	}

	dms, err := store.ListDirectConversations()
	if err != nil {
		t.Fatalf("ListDirectConversations() error = %v", err)
	}
	if len(dms) != 1 || dms[0].ID != "dm_pg" || len(dms[0].ParticipantIDs) != 2 {
		t.Fatalf("unexpected dm conversations %#v", dms)
	}
	dmPage, err := store.ListDMMessages("dm_pg", "", 10)
	if err != nil {
		t.Fatalf("ListDMMessages() error = %v", err)
	}
	if len(dmPage.Messages) != 1 || dmPage.Messages[0].ID != "msg_dm_1" {
		t.Fatalf("unexpected dm page %#v", dmPage)
	}

	threadArtifacts, err := store.ListArtifacts(protocol.ArtifactFilter{ThreadID: "thread_1"}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts(thread) error = %v", err)
	}
	if len(threadArtifacts.Artifacts) != 1 || threadArtifacts.Artifacts[0].Filename != "mock.png" {
		t.Fatalf("unexpected thread artifacts %#v", threadArtifacts)
	}
	dmArtifacts, err := store.ListArtifacts(protocol.ArtifactFilter{DMID: "dm_pg"}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts(dm) error = %v", err)
	}
	if len(dmArtifacts.Artifacts) != 1 || dmArtifacts.Artifacts[0].Filename != "note.mp3" {
		t.Fatalf("unexpected dm artifacts %#v", dmArtifacts)
	}

	if _, ok, err := store.GetThread("missing"); err != nil || ok {
		t.Fatalf("expected missing thread lookup to fail, ok=%v err=%v", ok, err)
	}
}
