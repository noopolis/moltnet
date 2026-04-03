package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSQLiteStoreClosedDatabaseFallbacks(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}

	if err := store.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if rooms, err := store.ListRooms(); err == nil || rooms != nil {
		t.Fatalf("expected ListRooms() error, got rooms=%#v err=%v", rooms, err)
	}
	if members := queryRoomMembers(t, store, "research"); members != nil {
		t.Fatalf("expected nil members, got %#v", members)
	}
	if _, ok, err := store.GetRoom("research"); err == nil || ok {
		t.Fatalf("expected GetRoom() error on closed db, ok=%v err=%v", ok, err)
	}
	if threads, err := store.ListThreads("research"); err == nil || threads != nil {
		t.Fatalf("expected ListThreads() error, got threads=%#v err=%v", threads, err)
	}
	if _, ok, err := store.GetThread("thread_1"); err == nil || ok {
		t.Fatalf("expected GetThread() error on closed db, ok=%v err=%v", ok, err)
	}
	if page, err := store.ListRoomMessages("research", "", 10); err == nil || len(page.Messages) != 0 || page.Page.HasMore {
		t.Fatalf("expected ListRoomMessages() error, got page=%#v err=%v", page, err)
	}
	if page, err := store.ListThreadMessages("thread_1", "", 10); err == nil || len(page.Messages) != 0 || page.Page.HasMore {
		t.Fatalf("expected ListThreadMessages() error, got page=%#v err=%v", page, err)
	}
	if dms, err := store.ListDirectConversations(); err == nil || dms != nil {
		t.Fatalf("expected ListDirectConversations() error, got dms=%#v err=%v", dms, err)
	}
	if participants := queryDMParticipants(t, store, "dm_1"); participants != nil {
		t.Fatalf("expected nil participants, got %#v", participants)
	}
	if page, err := store.ListDMMessages("dm_1", "", 10); err == nil || len(page.Messages) != 0 || page.Page.HasMore {
		t.Fatalf("expected ListDMMessages() error, got page=%#v err=%v", page, err)
	}
	if page, err := store.ListArtifacts(protocol.ArtifactFilter{RoomID: "research"}, "", 10); err == nil || len(page.Artifacts) != 0 || page.Page.HasMore {
		t.Fatalf("expected ListArtifacts() error, got page=%#v err=%v", page, err)
	}
	if ok, err := messageCursorExistsContext(context.Background(), store.db, store.dialect, "msg_1"); err == nil || ok {
		t.Fatalf("expected closed-db message cursor error, got ok=%v err=%v", ok, err)
	}
	if ok, err := artifactCursorExistsContext(context.Background(), store.db, store.dialect, "art_1"); err == nil || ok {
		t.Fatalf("expected closed-db artifact cursor error, got ok=%v err=%v", ok, err)
	}

	room := protocol.Room{
		ID:        "research",
		NetworkID: "local",
		FQID:      protocol.RoomFQID("local", "research"),
		Name:      "Research",
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateRoom(room); err == nil {
		t.Fatal("expected CreateRoom() error on closed db")
	}

	message := protocol.Message{
		ID:        "msg_1",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "alpha"},
		Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.AppendMessage(message); err == nil {
		t.Fatal("expected AppendMessage() error on closed db")
	}
}

func TestSQLiteStoreArtifactFiltersAndDefaultPagination(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	room := protocol.Room{
		ID:        "research",
		NetworkID: "local",
		FQID:      protocol.RoomFQID("local", "research"),
		Name:      "Research",
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	now := time.Now().UTC()
	messages := []protocol.Message{
		{
			ID:        "room_artifact",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{ID: "alpha"},
			Parts:     []protocol.Part{{Kind: "file", URL: "https://example.com/brief.pdf", Filename: "brief.pdf", MediaType: "application/pdf"}},
			CreatedAt: now,
		},
		{
			ID:        "thread_artifact",
			NetworkID: "local",
			Target: protocol.Target{
				Kind:            protocol.TargetKindThread,
				RoomID:          "research",
				ThreadID:        "thread_1",
				ParentMessageID: "room_artifact",
			},
			From:      protocol.Actor{ID: "beta"},
			Parts:     []protocol.Part{{Kind: "image", URL: "https://example.com/diagram.png", Filename: "diagram.png", MediaType: "image/png"}},
			CreatedAt: now.Add(time.Second),
		},
		{
			ID:        "dm_artifact",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_pair", ParticipantIDs: []string{"alpha", "beta"}},
			From:      protocol.Actor{ID: "alpha"},
			Parts:     []protocol.Part{{Kind: "audio", URL: "https://example.com/note.mp3", Filename: "note.mp3", MediaType: "audio/mpeg"}},
			CreatedAt: now.Add(2 * time.Second),
		},
	}
	for _, message := range messages {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	if _, err := store.ListRoomMessages("research", "missing", 0); err != ErrInvalidCursor {
		t.Fatalf("expected ErrInvalidCursor for room page, got %v", err)
	}

	if _, err := store.ListArtifacts(protocol.ArtifactFilter{ThreadID: "thread_1"}, "missing", 0); err != ErrInvalidCursor {
		t.Fatalf("expected ErrInvalidCursor for thread artifacts, got %v", err)
	}

	if _, err := store.ListArtifacts(protocol.ArtifactFilter{DMID: "dm_pair"}, "missing", 0); err != ErrInvalidCursor {
		t.Fatalf("expected ErrInvalidCursor for dm artifacts, got %v", err)
	}

	if _, err := store.ListArtifacts(protocol.ArtifactFilter{}, "missing", 0); err != ErrInvalidCursor {
		t.Fatalf("expected ErrInvalidCursor for all artifacts, got %v", err)
	}
}
