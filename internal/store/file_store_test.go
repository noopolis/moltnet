package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestFileStorePersistsRoomsAndMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	room := protocol.Room{ID: "research", NetworkID: "local", FQID: protocol.RoomFQID("local", "research"), Name: "Research"}
	if err := store.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	now := time.Now().UTC()
	if err := store.AppendMessage(protocol.Message{
		ID:        "msg_1",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "orchestrator"},
		Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	reloaded, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() reload error = %v", err)
	}

	rooms, err := reloaded.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms() error = %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID != "research" {
		t.Fatalf("unexpected rooms %#v", rooms)
	}

	page, err := reloaded.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "msg_1" {
		t.Fatalf("unexpected messages %#v", page)
	}

	if err := reloaded.AppendMessageContext(t.Context(), protocol.Message{
		ID:        "msg_2",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "researcher"},
		Parts:     []protocol.Part{{Kind: "text", Text: "follow up"}},
		CreatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("AppendMessageContext() error = %v", err)
	}

	afterPage, err := reloaded.ListRoomMessagesContext(t.Context(), "research", protocol.PageRequest{
		After: "msg_1",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListRoomMessagesContext() after error = %v", err)
	}
	if len(afterPage.Messages) != 1 || afterPage.Messages[0].ID != "msg_2" {
		t.Fatalf("unexpected after page %#v", afterPage)
	}
}

func TestFileStorePersistsThreads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	now := time.Now().UTC()
	if err := store.AppendMessage(protocol.Message{
		ID:        "msg_2",
		NetworkID: "local",
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_1",
			ParentMessageID: "msg_parent",
		},
		From:      protocol.Actor{ID: "writer"},
		Parts:     []protocol.Part{{Kind: "text", Text: "thread reply"}},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendMessage() thread error = %v", err)
	}

	reloaded, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() reload error = %v", err)
	}

	threads, err := reloaded.ListThreads("research")
	if err != nil {
		t.Fatalf("ListThreads() error = %v", err)
	}
	if len(threads) != 1 || threads[0].ID != "thread_1" || threads[0].ParentMessageID != "msg_parent" {
		t.Fatalf("unexpected threads %#v", threads)
	}

	page, err := reloaded.ListThreadMessages("thread_1", "", 10)
	if err != nil {
		t.Fatalf("ListThreadMessages() error = %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "msg_2" {
		t.Fatalf("unexpected thread messages %#v", page)
	}
}

func TestFileStoreSecuresParentDirectory(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(directory, "state.json")

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	room := protocol.Room{ID: "research", NetworkID: "local", FQID: protocol.RoomFQID("local", "research"), Name: "Research"}
	if err := store.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	info, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected secure directory permissions, got %o", got)
	}
}

func TestFileStoreContextWrappersAndMemberUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	room := protocol.Room{
		ID:        "research",
		NetworkID: "local",
		FQID:      protocol.RoomFQID("local", "research"),
		Name:      "Research",
		Members:   []string{"alpha", "beta"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateRoomContext(t.Context(), room); err != nil {
		t.Fatalf("CreateRoomContext() error = %v", err)
	}

	updated, err := store.UpdateRoomMembers("research", []string{"gamma"}, []string{"beta"})
	if err != nil {
		t.Fatalf("UpdateRoomMembers() error = %v", err)
	}
	if len(updated.Members) != 2 || updated.Members[1] != "gamma" {
		t.Fatalf("unexpected updated room %#v", updated)
	}

	now := time.Now().UTC()
	for _, message := range []protocol.Message{
		{
			ID:        "msg_room",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{ID: "alpha"},
			Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
			CreatedAt: now,
		},
		{
			ID:        "msg_thread",
			NetworkID: "local",
			Target: protocol.Target{
				Kind:            protocol.TargetKindThread,
				RoomID:          "research",
				ThreadID:        "thread_1",
				ParentMessageID: "msg_room",
			},
			From:      protocol.Actor{ID: "gamma"},
			Parts:     []protocol.Part{{Kind: protocol.PartKindImage, URL: "https://example.com/mock.png", Filename: "mock.png", MediaType: "image/png"}},
			CreatedAt: now.Add(time.Second),
		},
		{
			ID:        "msg_dm",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"alpha", "gamma"}},
			From:      protocol.Actor{ID: "alpha"},
			Parts:     []protocol.Part{{Kind: protocol.PartKindAudio, URL: "https://example.com/note.mp3", Filename: "note.mp3", MediaType: "audio/mpeg"}},
			CreatedAt: now.Add(2 * time.Second),
		},
	} {
		if err := store.AppendMessageContext(t.Context(), message); err != nil {
			t.Fatalf("AppendMessageContext(%s) error = %v", message.ID, err)
		}
	}

	gotRoom, ok, err := store.GetRoomContext(t.Context(), "research")
	if err != nil || !ok || len(gotRoom.Members) != 2 {
		t.Fatalf("unexpected room %#v ok=%v err=%v", gotRoom, ok, err)
	}
	if _, ok, err := store.GetThread("thread_1"); err != nil || !ok {
		if err != nil {
			t.Fatalf("GetThread() error = %v", err)
		}
		t.Fatal("expected thread lookup to succeed")
	}
	thread, ok, err := store.GetThreadContext(t.Context(), "thread_1")
	if err != nil || !ok || thread.ID != "thread_1" {
		t.Fatalf("unexpected thread %#v ok=%v err=%v", thread, ok, err)
	}

	rooms, err := store.ListRoomsContext(t.Context())
	if err != nil || len(rooms) != 1 {
		t.Fatalf("unexpected rooms %#v err=%v", rooms, err)
	}
	threads, err := store.ListThreadsContext(t.Context(), "research")
	if err != nil || len(threads) != 1 {
		t.Fatalf("unexpected threads %#v err=%v", threads, err)
	}
	threadPage, err := store.ListThreadMessagesContext(t.Context(), "thread_1", protocol.PageRequest{Limit: 10})
	if err != nil || len(threadPage.Messages) != 1 || threadPage.Messages[0].ID != "msg_thread" {
		t.Fatalf("unexpected thread page %#v err=%v", threadPage, err)
	}

	dms, err := store.ListDirectConversationsContext(t.Context())
	if err != nil || len(dms) != 1 || dms[0].ID != "dm_1" {
		t.Fatalf("unexpected direct conversations %#v err=%v", dms, err)
	}
	dm, ok, err := store.GetDirectConversationContext(t.Context(), "dm_1")
	if err != nil || !ok || len(dm.ParticipantIDs) != 2 {
		t.Fatalf("unexpected direct conversation %#v ok=%v err=%v", dm, ok, err)
	}
	if dmPage, err := store.ListDMMessages("dm_1", "", 10); err != nil || len(dmPage.Messages) != 1 || dmPage.Messages[0].ID != "msg_dm" {
		t.Fatalf("unexpected dm page %#v err=%v", dmPage, err)
	}
	dmPage, err := store.ListDMMessagesContext(t.Context(), "dm_1", protocol.PageRequest{Limit: 10})
	if err != nil || len(dmPage.Messages) != 1 || dmPage.Messages[0].ID != "msg_dm" {
		t.Fatalf("unexpected dm page %#v err=%v", dmPage, err)
	}

	if artifactPage, err := store.ListArtifacts(protocol.ArtifactFilter{RoomID: "research"}, "", 10); err != nil || len(artifactPage.Artifacts) != 1 {
		t.Fatalf("unexpected artifact page %#v err=%v", artifactPage, err)
	}
	artifactPage, err := store.ListArtifactsContext(t.Context(), protocol.ArtifactFilter{DMID: "dm_1"}, protocol.PageRequest{Limit: 10})
	if err != nil || len(artifactPage.Artifacts) != 1 || artifactPage.Artifacts[0].MessageID != "msg_dm" {
		t.Fatalf("unexpected artifact page %#v err=%v", artifactPage, err)
	}
}
