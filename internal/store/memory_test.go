package store

import (
	"errors"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMemoryStoreRooms(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()

	roomA := protocol.Room{ID: "b-room", Name: "B"}
	roomB := protocol.Room{ID: "a-room", Name: "A"}

	if err := store.CreateRoom(roomA); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	if err := store.CreateRoom(roomB); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	if err := store.CreateRoom(roomA); err == nil {
		t.Fatal("expected duplicate room error")
	} else if !errors.Is(err, ErrRoomExists) {
		t.Fatalf("expected ErrRoomExists, got %v", err)
	}

	room, ok, err := store.GetRoom("a-room")
	if err != nil || !ok || room.ID != "a-room" {
		t.Fatalf("GetRoom() = %#v, %v, %v", room, ok, err)
	}

	rooms, err := store.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms() error = %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("expected 2 rooms, got %d", len(rooms))
	}

	if rooms[0].ID != "a-room" || rooms[1].ID != "b-room" {
		t.Fatalf("rooms not sorted: %#v", rooms)
	}
}

func TestMemoryStoreHistory(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	now := time.Now().UTC()

	roomMessage1 := protocol.Message{
		ID:        "msg_1",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "orchestrator"},
		CreatedAt: now,
	}
	roomMessage2 := protocol.Message{
		ID:        "msg_2",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "researcher"},
		CreatedAt: now.Add(time.Second),
	}
	dmMessage1 := protocol.Message{
		ID:        "msg_3",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"researcher", "writer"}},
		From:      protocol.Actor{ID: "researcher"},
		CreatedAt: now.Add(2 * time.Second),
	}
	dmMessage2 := protocol.Message{
		ID:        "msg_4",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"researcher", "writer"}},
		From:      protocol.Actor{ID: "writer"},
		CreatedAt: now.Add(3 * time.Second),
	}

	for _, message := range []protocol.Message{roomMessage1, roomMessage2, dmMessage1, dmMessage2} {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	roomPage, err := store.ListRoomMessages("research", "", 1)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(roomPage.Messages) != 1 || roomPage.Messages[0].ID != "msg_2" {
		t.Fatalf("unexpected room messages: %#v", roomPage)
	}
	if !roomPage.Page.HasMore || roomPage.Page.NextBefore != "msg_2" {
		t.Fatalf("unexpected room page info: %#v", roomPage.Page)
	}

	dms, err := store.ListDirectConversations()
	if err != nil {
		t.Fatalf("ListDirectConversations() error = %v", err)
	}
	if len(dms) != 1 {
		t.Fatalf("expected 1 dm conversation, got %d", len(dms))
	}

	if dms[0].ID != "dm_1" || dms[0].MessageCount != 2 {
		t.Fatalf("unexpected dm conversation: %#v", dms[0])
	}
	if dms[0].FQID != "molt://local/dms/dm_1" {
		t.Fatalf("unexpected dm fqid %q", dms[0].FQID)
	}

	if len(dms[0].ParticipantIDs) != 2 || dms[0].ParticipantIDs[0] != "researcher" || dms[0].ParticipantIDs[1] != "writer" {
		t.Fatalf("unexpected participants: %#v", dms[0].ParticipantIDs)
	}

	dmPage, err := store.ListDMMessages("dm_1", "", 10)
	if err != nil {
		t.Fatalf("ListDMMessages() error = %v", err)
	}
	if len(dmPage.Messages) != 2 || dmPage.Messages[0].ID != "msg_3" || dmPage.Messages[1].ID != "msg_4" {
		t.Fatalf("unexpected dm messages: %#v", dmPage)
	}

	pagedDMs, err := store.ListDMMessages("dm_1", "msg_4", 1)
	if err != nil {
		t.Fatalf("ListDMMessages(paged) error = %v", err)
	}
	if len(pagedDMs.Messages) != 1 || pagedDMs.Messages[0].ID != "msg_3" || pagedDMs.Page.HasMore {
		t.Fatalf("unexpected paged dm messages: %#v", pagedDMs)
	}

	afterRoomPage, err := store.ListRoomMessagesContext(t.Context(), "research", protocol.PageRequest{
		After: "msg_1",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListRoomMessagesContext() after error = %v", err)
	}
	if len(afterRoomPage.Messages) != 1 || afterRoomPage.Messages[0].ID != "msg_2" || afterRoomPage.Page.HasMore {
		t.Fatalf("unexpected after room page %#v", afterRoomPage)
	}

	directConversation, ok, err := store.GetDirectConversationContext(t.Context(), "dm_1")
	if err != nil {
		t.Fatalf("GetDirectConversationContext() error = %v", err)
	}
	if !ok || directConversation.ID != "dm_1" {
		t.Fatalf("unexpected direct conversation %#v ok=%v", directConversation, ok)
	}
}

func TestMemoryStoreThreadsAndArtifacts(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	now := time.Now().UTC()

	if err := store.AppendMessage(protocol.Message{
		ID:        "msg_thread_1",
		NetworkID: "local",
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_1",
			ParentMessageID: "msg_parent",
		},
		From: protocol.Actor{ID: "writer"},
		Parts: []protocol.Part{
			{Kind: "text", Text: "reply"},
			{Kind: "image", URL: "https://example.com/mock.png", Filename: "mock.png", MediaType: "image/png"},
		},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendMessage() thread error = %v", err)
	}

	thread, ok, err := store.GetThread("thread_1")
	if err != nil || !ok || thread.RoomID != "research" || thread.ParentMessageID != "msg_parent" {
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
	if len(threadPage.Messages) != 1 || threadPage.Messages[0].ID != "msg_thread_1" {
		t.Fatalf("unexpected thread page %#v", threadPage)
	}

	artifactPage, err := store.ListArtifacts(protocol.ArtifactFilter{ThreadID: "thread_1"}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(artifactPage.Artifacts) != 1 || artifactPage.Artifacts[0].MessageID != "msg_thread_1" {
		t.Fatalf("unexpected artifacts %#v", artifactPage)
	}
	if artifactPage.Artifacts[0].FQID != "molt://local/artifacts/art_msg_thread_1_1" {
		t.Fatalf("unexpected artifact fqid %q", artifactPage.Artifacts[0].FQID)
	}
}

func TestMemoryStoreContextWrappersAndRoomMemberUpdates(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
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

	gotRoom, ok, err := store.GetRoomContext(t.Context(), "research")
	if err != nil || !ok || len(gotRoom.Members) != 2 {
		t.Fatalf("unexpected room %#v ok=%v err=%v", gotRoom, ok, err)
	}

	updated, err := store.UpdateRoomMembers("research", []string{"gamma"}, []string{"beta"})
	if err != nil {
		t.Fatalf("UpdateRoomMembers() error = %v", err)
	}
	if len(updated.Members) != 2 || updated.Members[0] != "alpha" || updated.Members[1] != "gamma" {
		t.Fatalf("unexpected updated room %#v", updated)
	}

	rooms, err := store.ListRoomsContext(t.Context())
	if err != nil || len(rooms) != 1 || rooms[0].ID != "research" {
		t.Fatalf("unexpected rooms %#v err=%v", rooms, err)
	}

	message := protocol.Message{
		ID:        "msg_thread_ctx",
		NetworkID: "local",
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_ctx",
			ParentMessageID: "msg_parent",
		},
		From:      protocol.Actor{ID: "alpha"},
		Parts:     []protocol.Part{{Kind: protocol.PartKindText, Text: "context"}},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.AppendMessageContext(t.Context(), message); err != nil {
		t.Fatalf("AppendMessageContext() error = %v", err)
	}

	thread, ok, err := store.GetThreadContext(t.Context(), "thread_ctx")
	if err != nil || !ok || thread.ID != "thread_ctx" {
		t.Fatalf("unexpected thread %#v ok=%v err=%v", thread, ok, err)
	}
}

func TestMemoryStoreAgentQueries(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
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
	if _, ok, err := store.GetAgentContext(t.Context(), "   "); err != nil || ok {
		t.Fatalf("expected blank agent id to miss, got ok=%v err=%v", ok, err)
	}
}
