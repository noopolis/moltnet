package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSQLiteStoreRoomsMessagesThreadsArtifacts(t *testing.T) {
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
		Members:   []string{"orchestrator", "writer"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	if err := store.CreateRoom(room); err == nil {
		t.Fatal("expected duplicate room error")
	} else if !errors.Is(err, ErrRoomExists) {
		t.Fatalf("expected ErrRoomExists, got %v", err)
	}

	reloadedRoom, ok, err := store.GetRoom("research")
	if err != nil || !ok || len(reloadedRoom.Members) != 2 {
		t.Fatalf("unexpected room %#v ok=%v err=%v", reloadedRoom, ok, err)
	}

	now := time.Now().UTC()
	messages := []protocol.Message{
		{
			ID:        "msg_room",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "orchestrator"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
			Mentions:  []string{"writer"},
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
			From:      protocol.Actor{Type: "agent", ID: "writer"},
			Parts:     []protocol.Part{{Kind: "image", URL: "https://example.com/mock.png", Filename: "mock.png", MediaType: "image/png"}},
			CreatedAt: now.Add(time.Second),
		},
		{
			ID:        "msg_dm",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"alpha", "beta"}},
			From:      protocol.Actor{Type: "agent", ID: "alpha"},
			Parts:     []protocol.Part{{Kind: "audio", URL: "https://example.com/note.mp3", Filename: "note.mp3", MediaType: "audio/mpeg"}},
			CreatedAt: now.Add(2 * time.Second),
		},
	}
	for _, message := range messages {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	roomPage, err := store.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(roomPage.Messages) != 1 || roomPage.Messages[0].ID != "msg_room" {
		t.Fatalf("unexpected room page %#v", roomPage)
	}

	thread, ok, err := store.GetThread("thread_1")
	if err != nil || !ok || thread.ParentMessageID != "msg_room" || thread.MessageCount != 1 {
		t.Fatalf("unexpected thread %#v ok=%v err=%v", thread, ok, err)
	}

	threads, err := store.ListThreads("research")
	if err != nil {
		t.Fatalf("ListThreads() error = %v", err)
	}
	if len(threads) != 1 || threads[0].ID != "thread_1" {
		t.Fatalf("unexpected threads %#v", threads)
	}

	threadPage, err := store.ListThreadMessages("thread_1", "", 10)
	if err != nil {
		t.Fatalf("ListThreadMessages() error = %v", err)
	}
	if len(threadPage.Messages) != 1 || threadPage.Messages[0].ID != "msg_thread" {
		t.Fatalf("unexpected thread page %#v", threadPage)
	}

	dms, err := store.ListDirectConversations()
	if err != nil {
		t.Fatalf("ListDirectConversations() error = %v", err)
	}
	if len(dms) != 1 || len(dms[0].ParticipantIDs) != 2 || dms[0].FQID != protocol.DMFQID("local", "dm_1") {
		t.Fatalf("unexpected dm conversations %#v", dms)
	}

	dmPage, err := store.ListDMMessages("dm_1", "", 10)
	if err != nil {
		t.Fatalf("ListDMMessages() error = %v", err)
	}
	if len(dmPage.Messages) != 1 || dmPage.Messages[0].ID != "msg_dm" {
		t.Fatalf("unexpected dm page %#v", dmPage)
	}

	artifactPage, err := store.ListArtifacts(protocol.ArtifactFilter{RoomID: "research"}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(artifactPage.Artifacts) != 1 || artifactPage.Artifacts[0].Filename != "mock.png" {
		t.Fatalf("unexpected artifacts %#v", artifactPage)
	}
}

func TestSQLiteStorePaginationAndMigrationIdempotency(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "moltnet.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}

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
	for _, message := range []protocol.Message{
		{ID: "msg_1", NetworkID: "local", Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}, From: protocol.Actor{ID: "one"}, Parts: []protocol.Part{{Kind: "text", Text: "one"}}, CreatedAt: now},
		{ID: "msg_2", NetworkID: "local", Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}, From: protocol.Actor{ID: "two"}, Parts: []protocol.Part{{Kind: "text", Text: "two"}}, CreatedAt: now.Add(time.Second)},
		{ID: "msg_3", NetworkID: "local", Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}, From: protocol.Actor{ID: "three"}, Parts: []protocol.Part{{Kind: "text", Text: "three"}}, CreatedAt: now.Add(2 * time.Second)},
	} {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	firstPage, err := store.ListRoomMessages("research", "", 2)
	if err != nil {
		t.Fatalf("ListRoomMessages(first) error = %v", err)
	}
	if len(firstPage.Messages) != 2 || firstPage.Messages[0].ID != "msg_2" || !firstPage.Page.HasMore {
		t.Fatalf("unexpected first page %#v", firstPage)
	}
	secondPage, err := store.ListRoomMessages("research", firstPage.Page.NextBefore, 2)
	if err != nil {
		t.Fatalf("ListRoomMessages(second) error = %v", err)
	}
	if len(secondPage.Messages) != 1 || secondPage.Messages[0].ID != "msg_1" || secondPage.Page.HasMore {
		t.Fatalf("unexpected second page %#v", secondPage)
	}

	if queryCount(t, store.db, store.dialect, `SELECT COUNT(1) FROM schema_migrations`) != len(sqlMigrations) {
		t.Fatalf("expected %d applied migrations", len(sqlMigrations))
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()
	if queryCount(t, reopened.db, reopened.dialect, `SELECT COUNT(1) FROM schema_migrations`) != len(sqlMigrations) {
		t.Fatalf("expected idempotent migrations")
	}
}

func TestBindQuery(t *testing.T) {
	t.Parallel()

	sqlite := bindQuery(dialectSQLite, `SELECT * FROM rooms WHERE id = ? AND name = ?`)
	if sqlite != `SELECT * FROM rooms WHERE id = ? AND name = ?` {
		t.Fatalf("unexpected sqlite query %q", sqlite)
	}

	postgres := bindQuery(dialectPostgres, `SELECT * FROM rooms WHERE id = ? AND name = ?`)
	if postgres != `SELECT * FROM rooms WHERE id = $1 AND name = $2` {
		t.Fatalf("unexpected postgres query %q", postgres)
	}
}

func TestSQLiteStoreSecuresParentDirectory(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "db")
	path := filepath.Join(directory, "moltnet.db")

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	info, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected secure directory permissions, got %o", got)
	}
}

func TestSQLiteStoreContextWrappersAndRoomMemberUpdates(t *testing.T) {
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
		Members:   []string{"alpha", "beta"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateRoomContext(t.Context(), room); err != nil {
		t.Fatalf("CreateRoomContext() error = %v", err)
	}

	updated, err := store.UpdateRoomMembersContext(t.Context(), "research", []string{"gamma"}, []string{"beta"})
	if err != nil {
		t.Fatalf("UpdateRoomMembersContext() error = %v", err)
	}
	if len(updated.Members) != 2 || updated.Members[1] != "gamma" {
		t.Fatalf("unexpected updated room %#v", updated)
	}
	updated, err = store.UpdateRoomMembers("research", []string{"beta"}, nil)
	if err != nil || len(updated.Members) != 3 {
		t.Fatalf("unexpected UpdateRoomMembers() result %#v err=%v", updated, err)
	}

	gotRoom, ok, err := store.GetRoomContext(t.Context(), "research")
	if err != nil || !ok || len(gotRoom.Members) != 3 {
		t.Fatalf("unexpected room %#v ok=%v err=%v", gotRoom, ok, err)
	}

	now := time.Now().UTC()
	message := protocol.Message{
		ID:        "msg_dm_ctx",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_ctx", ParticipantIDs: []string{"alpha", "gamma"}},
		From:      protocol.Actor{ID: "alpha"},
		Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
		CreatedAt: now,
	}
	if err := store.AppendMessageContext(t.Context(), message); err != nil {
		t.Fatalf("AppendMessageContext() error = %v", err)
	}
	if err := store.AppendMessageContext(t.Context(), message); !errors.Is(err, ErrDuplicateMessage) {
		t.Fatalf("expected ErrDuplicateMessage, got %v", err)
	}

	dms, err := store.ListDirectConversationsContext(t.Context())
	if err != nil || len(dms) != 1 || dms[0].ID != "dm_ctx" {
		t.Fatalf("unexpected dms %#v err=%v", dms, err)
	}
	dm, ok, err := store.GetDirectConversationContext(t.Context(), "dm_ctx")
	if err != nil || !ok || len(dm.ParticipantIDs) != 2 {
		t.Fatalf("unexpected direct conversation %#v ok=%v err=%v", dm, ok, err)
	}
}

func TestSQLiteStoreAgentQueries(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	for _, room := range []protocol.Room{
		{
			ID:        "alpha",
			NetworkID: "local",
			FQID:      protocol.RoomFQID("local", "alpha"),
			Name:      "Alpha",
			Members:   []string{"writer", "reviewer"},
			CreatedAt: now,
		},
		{
			ID:        "beta",
			NetworkID: "local",
			FQID:      protocol.RoomFQID("local", "beta"),
			Name:      "Beta",
			Members:   []string{"writer", "editor"},
			CreatedAt: now.Add(time.Second),
		},
	} {
		if err := store.CreateRoomContext(t.Context(), room); err != nil {
			t.Fatalf("CreateRoomContext() error = %v", err)
		}
	}

	agents, err := store.ListAgentsContext(t.Context())
	if err != nil {
		t.Fatalf("ListAgentsContext() error = %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %#v", agents)
	}
	if agents[0].ID != "editor" || agents[1].ID != "reviewer" || agents[2].ID != "writer" {
		t.Fatalf("expected sorted agents, got %#v", agents)
	}
	if agents[2].FQID != protocol.AgentFQID("local", "writer") || len(agents[2].Rooms) != 2 {
		t.Fatalf("unexpected writer summary %#v", agents[2])
	}
	if agents[2].Rooms[0] != "alpha" || agents[2].Rooms[1] != "beta" {
		t.Fatalf("expected sorted writer rooms, got %#v", agents[2].Rooms)
	}

	agent, ok, err := store.GetAgentContext(t.Context(), "writer")
	if err != nil {
		t.Fatalf("GetAgentContext() error = %v", err)
	}
	if !ok || agent.ID != "writer" || len(agent.Rooms) != 2 {
		t.Fatalf("unexpected agent %#v ok=%v", agent, ok)
	}

	if _, ok, err := store.GetAgentContext(t.Context(), "missing"); err != nil || ok {
		t.Fatalf("expected missing agent, got ok=%v err=%v", ok, err)
	}
}
