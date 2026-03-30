package store

import (
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
	}

	room, ok := store.GetRoom("a-room")
	if !ok || room.ID != "a-room" {
		t.Fatalf("GetRoom() = %#v, %v", room, ok)
	}

	rooms := store.ListRooms()
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

	roomPage := store.ListRoomMessages("research", "", 1)
	if len(roomPage.Messages) != 1 || roomPage.Messages[0].ID != "msg_2" {
		t.Fatalf("unexpected room messages: %#v", roomPage)
	}
	if !roomPage.Page.HasMore || roomPage.Page.NextBefore != "msg_2" {
		t.Fatalf("unexpected room page info: %#v", roomPage.Page)
	}

	dms := store.ListDirectConversations()
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

	dmPage := store.ListDMMessages("dm_1", "", 10)
	if len(dmPage.Messages) != 2 || dmPage.Messages[0].ID != "msg_3" || dmPage.Messages[1].ID != "msg_4" {
		t.Fatalf("unexpected dm messages: %#v", dmPage)
	}

	pagedDMs := store.ListDMMessages("dm_1", "msg_4", 1)
	if len(pagedDMs.Messages) != 1 || pagedDMs.Messages[0].ID != "msg_3" || pagedDMs.Page.HasMore {
		t.Fatalf("unexpected paged dm messages: %#v", pagedDMs)
	}
}
