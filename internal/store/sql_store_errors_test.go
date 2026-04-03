package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSQLiteStoreListRoomsAndArtifactsPagination(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	rooms := []protocol.Room{
		{ID: "b-room", NetworkID: "local", FQID: protocol.RoomFQID("local", "b-room"), Name: "B", CreatedAt: time.Now().UTC()},
		{ID: "a-room", NetworkID: "local", FQID: protocol.RoomFQID("local", "a-room"), Name: "A", CreatedAt: time.Now().UTC()},
	}
	for _, room := range rooms {
		if err := store.CreateRoom(room); err != nil {
			t.Fatalf("CreateRoom() error = %v", err)
		}
	}

	listed, err := store.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms() error = %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "a-room" || listed[1].ID != "b-room" {
		t.Fatalf("unexpected sorted rooms %#v", listed)
	}

	now := time.Now().UTC()
	for _, message := range []protocol.Message{
		{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "a-room"},
			From:      protocol.Actor{ID: "alpha"},
			Parts:     []protocol.Part{{Kind: "file", URL: "https://example.com/one.pdf", Filename: "one.pdf"}},
			CreatedAt: now,
		},
		{
			ID:        "msg_2",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "a-room"},
			From:      protocol.Actor{ID: "beta"},
			Parts:     []protocol.Part{{Kind: "image", URL: "https://example.com/two.png", Filename: "two.png"}},
			CreatedAt: now.Add(time.Second),
		},
	} {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	firstPage, err := store.ListArtifacts(protocol.ArtifactFilter{RoomID: "a-room"}, "", 1)
	if err != nil {
		t.Fatalf("ListArtifacts(first) error = %v", err)
	}
	if len(firstPage.Artifacts) != 1 || firstPage.Artifacts[0].MessageID != "msg_2" || !firstPage.Page.HasMore {
		t.Fatalf("unexpected first artifact page %#v", firstPage)
	}
	secondPage, err := store.ListArtifacts(protocol.ArtifactFilter{RoomID: "a-room"}, firstPage.Page.NextBefore, 1)
	if err != nil {
		t.Fatalf("ListArtifacts(second) error = %v", err)
	}
	if len(secondPage.Artifacts) != 1 || secondPage.Artifacts[0].MessageID != "msg_1" || secondPage.Page.HasMore {
		t.Fatalf("unexpected second artifact page %#v", secondPage)
	}

	if ok := queryArtifactCursorExists(t, store, firstPage.Page.NextBefore); !ok {
		t.Fatalf("expected artifact cursor lookup for %q", firstPage.Page.NextBefore)
	}
}

func TestSQLiteStoreAfterPaginationPaths(t *testing.T) {
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
	for _, message := range []protocol.Message{
		{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{ID: "alpha"},
			Parts:     []protocol.Part{{Kind: "file", URL: "https://example.com/one.pdf", Filename: "one.pdf"}},
			CreatedAt: now,
		},
		{
			ID:        "msg_2",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{ID: "beta"},
			Parts:     []protocol.Part{{Kind: "image", URL: "https://example.com/two.png", Filename: "two.png"}},
			CreatedAt: now.Add(time.Second),
		},
		{
			ID:        "msg_3",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{ID: "gamma"},
			Parts:     []protocol.Part{{Kind: "audio", URL: "https://example.com/three.mp3", Filename: "three.mp3"}},
			CreatedAt: now.Add(2 * time.Second),
		},
	} {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	messagePage, err := store.ListRoomMessagesContext(context.Background(), "research", protocol.PageRequest{
		After: "msg_1",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListRoomMessagesContext() after error = %v", err)
	}
	if len(messagePage.Messages) != 1 || messagePage.Messages[0].ID != "msg_2" || !messagePage.Page.HasMore || messagePage.Page.NextAfter != "msg_2" {
		t.Fatalf("unexpected after message page %#v", messagePage)
	}

	artifactPage, err := store.ListArtifactsContext(context.Background(), protocol.ArtifactFilter{RoomID: "research"}, protocol.PageRequest{
		After: "art_msg_1_0",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListArtifactsContext() after error = %v", err)
	}
	if len(artifactPage.Artifacts) != 1 || artifactPage.Artifacts[0].ID != "art_msg_2_0" || !artifactPage.Page.HasMore || artifactPage.Page.NextAfter != "art_msg_2_0" {
		t.Fatalf("unexpected after artifact page %#v", artifactPage)
	}

	_, err = store.ListRoomMessagesContext(context.Background(), "research", protocol.PageRequest{
		Before: "msg_3",
		After:  "msg_1",
		Limit:  1,
	})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor for mixed cursors, got %v", err)
	}
}

func TestSQLiteStoreSkipsBrokenRows(t *testing.T) {
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

	if _, err := store.db.Exec(`INSERT INTO messages (id, network_id, target_kind, room_id, target_json, from_json, parts_json, mentions_json, created_at) VALUES ('broken_message', 'local', 'room', 'research', '{', '{}', '[]', '[]', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert broken message: %v", err)
	}
	page, err := store.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 0 {
		t.Fatalf("expected broken message to be skipped, got %#v", page)
	}

	if _, err := store.db.Exec(`INSERT INTO messages (id, network_id, target_kind, room_id, target_json, from_json, parts_json, mentions_json, created_at) VALUES ('broken_actor', 'local', 'room', 'research', '{"kind":"room","room_id":"research"}', '{', '[]', '[]', '2026-01-01T00:00:01Z')`); err != nil {
		t.Fatalf("insert broken actor message: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO messages (id, network_id, target_kind, room_id, target_json, from_json, parts_json, mentions_json, created_at) VALUES ('broken_parts', 'local', 'room', 'research', '{"kind":"room","room_id":"research"}', '{"id":"alpha"}', '{', '[]', '2026-01-01T00:00:02Z')`); err != nil {
		t.Fatalf("insert broken parts message: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO messages (id, network_id, target_kind, room_id, target_json, from_json, parts_json, mentions_json, created_at) VALUES ('broken_mentions', 'local', 'room', 'research', '{"kind":"room","room_id":"research"}', '{"id":"alpha"}', '[]', '{', '2026-01-01T00:00:03Z')`); err != nil {
		t.Fatalf("insert broken mentions message: %v", err)
	}
	page, err = store.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages(second) error = %v", err)
	}
	if len(page.Messages) != 0 {
		t.Fatalf("expected malformed messages to be skipped, got %#v", page)
	}

	if _, err := store.db.Exec(`INSERT INTO messages (id, network_id, target_kind, room_id, target_json, from_json, parts_json, mentions_json, created_at, origin_json) VALUES ('msg_x', 'local', 'room', 'research', '{"kind":"room","room_id":"research"}', '{"id":"alpha"}', '[{"kind":"text","text":"ok"}]', '[]', '2026-01-01T00:00:04Z', '{}')`); err != nil {
		t.Fatalf("insert placeholder message: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO artifacts (id, network_id, fqid, message_id, target_kind, room_id, target_json, part_index, kind, created_at) VALUES ('broken_artifact', 'local', 'molt://local/artifacts/broken_artifact', 'msg_x', 'room', 'research', '{', 0, 'file', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert broken artifact: %v", err)
	}
	artifacts, err := store.ListArtifacts(protocol.ArtifactFilter{RoomID: "research"}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(artifacts.Artifacts) != 0 {
		t.Fatalf("expected broken artifact to be skipped, got %#v", artifacts)
	}
}

func TestPostgresStoreErrors(t *testing.T) {
	t.Parallel()

	if _, err := NewPostgresStore("postgres://invalid:://"); err == nil {
		t.Fatal("expected postgres open error")
	}
}

func TestSQLiteStoreMemberLookupsAndThreadPagination(t *testing.T) {
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
		Members:   []string{"beta", "alpha"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	parent := protocol.Message{
		ID:        "msg_parent",
		NetworkID: "local",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{ID: "alpha"},
		Parts:     []protocol.Part{{Kind: "text", Text: "parent"}},
		CreatedAt: time.Now().UTC().Add(-time.Second),
	}
	if err := store.AppendMessage(parent); err != nil {
		t.Fatalf("AppendMessage(parent) error = %v", err)
	}
	members := queryRoomMembers(t, store, "research")
	if len(members) != 2 || members[0] != "alpha" || members[1] != "beta" {
		t.Fatalf("unexpected room members %#v", members)
	}

	now := time.Now().UTC()
	for _, message := range []protocol.Message{
		{
			ID: "thread_1_msg_1", NetworkID: "local",
			Target: protocol.Target{Kind: protocol.TargetKindThread, RoomID: "research", ThreadID: "thread_1", ParentMessageID: "msg_parent"},
			From:   protocol.Actor{ID: "alpha"}, Parts: []protocol.Part{{Kind: "text", Text: "one"}}, CreatedAt: now,
		},
		{
			ID: "thread_1_msg_2", NetworkID: "local",
			Target: protocol.Target{Kind: protocol.TargetKindThread, RoomID: "research", ThreadID: "thread_1", ParentMessageID: "msg_parent"},
			From:   protocol.Actor{ID: "beta"}, Parts: []protocol.Part{{Kind: "text", Text: "two"}}, CreatedAt: now.Add(time.Second),
		},
		{
			ID: "dm_msg_1", NetworkID: "local",
			Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_pair", ParticipantIDs: []string{"alpha", "beta"}},
			From:   protocol.Actor{ID: "alpha"}, Parts: []protocol.Part{{Kind: "text", Text: "ping"}}, CreatedAt: now.Add(2 * time.Second),
		},
		{
			ID: "dm_msg_2", NetworkID: "local",
			Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_pair", ParticipantIDs: []string{"alpha", "beta"}},
			From:   protocol.Actor{ID: "beta"}, Parts: []protocol.Part{{Kind: "text", Text: "pong"}}, CreatedAt: now.Add(3 * time.Second),
		},
	} {
		if err := store.AppendMessage(message); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	threadPage, err := store.ListThreadMessages("thread_1", "", 1)
	if err != nil {
		t.Fatalf("ListThreadMessages(first) error = %v", err)
	}
	if len(threadPage.Messages) != 1 || threadPage.Messages[0].ID != "thread_1_msg_2" || !threadPage.Page.HasMore {
		t.Fatalf("unexpected thread page %#v", threadPage)
	}
	if ok := queryMessageCursorExists(t, store, threadPage.Page.NextBefore); !ok {
		t.Fatalf("expected thread message cursor for %q", threadPage.Page.NextBefore)
	}
	nextThreadPage, err := store.ListThreadMessages("thread_1", threadPage.Page.NextBefore, 1)
	if err != nil {
		t.Fatalf("ListThreadMessages(second) error = %v", err)
	}
	if len(nextThreadPage.Messages) != 1 || nextThreadPage.Messages[0].ID != "thread_1_msg_1" {
		t.Fatalf("unexpected next thread page %#v", nextThreadPage)
	}

	participants := queryDMParticipants(t, store, "dm_pair")
	if len(participants) != 2 || participants[0] != "alpha" || participants[1] != "beta" {
		t.Fatalf("unexpected dm participants %#v", participants)
	}

	allArtifacts, err := store.ListArtifacts(protocol.ArtifactFilter{}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts(all) error = %v", err)
	}
	if len(allArtifacts.Artifacts) != 0 {
		t.Fatalf("expected no artifacts from plain text messages, got %#v", allArtifacts)
	}

	if _, ok, err := store.GetThread("missing"); err != nil || ok {
		t.Fatalf("expected missing thread lookup to fail, ok=%v err=%v", ok, err)
	}
}
