package rooms

import (
	"context"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestServiceFallbackStorePaths(t *testing.T) {
	t.Parallel()

	service := newTestService()
	service.contextStore = nil
	service.contextMessages = nil
	service.lifecycleMessages = nil
	service.contextAgents = nil

	if err := service.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	room, err := service.CreateRoomContext(context.Background(), protocol.CreateRoomRequest{
		ID:      "research",
		Members: []string{"alpha", "beta"},
	})
	if err != nil {
		t.Fatalf("CreateRoomContext() error = %v", err)
	}

	gotRoom, err := service.GetRoom("research")
	if err != nil || gotRoom.ID != room.ID {
		t.Fatalf("unexpected room %#v err=%v", gotRoom, err)
	}

	updated, err := service.UpdateRoomMembers(context.Background(), "research", protocol.UpdateRoomMembersRequest{
		Add:    []string{"gamma"},
		Remove: []string{"beta"},
	})
	if err != nil || len(updated.Members) != 2 {
		t.Fatalf("unexpected updated room %#v err=%v", updated, err)
	}

	if _, err := service.SendMessageContext(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}); err != nil {
		t.Fatalf("SendMessageContext(room) error = %v", err)
	}
	threadAccepted, err := service.SendMessageContext(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_1",
			ParentMessageID: "msg_local_1",
		},
		From:  protocol.Actor{Type: "agent", ID: "gamma"},
		Parts: []protocol.Part{{Kind: protocol.PartKindImage, URL: "https://example.com/mock.png", Filename: "mock.png", MediaType: "image/png"}},
	})
	if err != nil {
		t.Fatalf("SendMessageContext(thread) error = %v", err)
	}
	if !threadAccepted.ThreadCreated {
		t.Fatalf("expected thread creation flag %#v", threadAccepted)
	}
	dmAccepted, err := service.SendMessageContext(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_1",
			ParticipantIDs: []string{"local:alpha", "local:gamma"},
		},
		From:  protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{{Kind: protocol.PartKindAudio, URL: "https://example.com/note.mp3", Filename: "note.mp3", MediaType: "audio/mpeg"}},
	})
	if err != nil {
		t.Fatalf("SendMessageContext(dm) error = %v", err)
	}
	if !dmAccepted.DMCreated {
		t.Fatalf("expected dm creation flag %#v", dmAccepted)
	}

	if page, err := service.ListRoomMessagesContext(context.Background(), "research", protocol.PageRequest{Limit: 10}); err != nil || len(page.Messages) != 1 {
		t.Fatalf("unexpected room page %#v err=%v", page, err)
	}
	if threadPage, err := service.ListThreadsContext(context.Background(), "research", protocol.PageRequest{Limit: 10}); err != nil || len(threadPage.Threads) != 1 {
		t.Fatalf("unexpected thread page %#v err=%v", threadPage, err)
	}
	if page, err := service.ListThreadMessagesContext(context.Background(), "thread_1", protocol.PageRequest{Limit: 10}); err != nil || len(page.Messages) != 1 {
		t.Fatalf("unexpected thread messages %#v err=%v", page, err)
	}
	if dms, err := service.ListDirectConversationsContext(context.Background(), protocol.PageRequest{Limit: 10}); err != nil || len(dms.DMs) != 1 {
		t.Fatalf("unexpected dms %#v err=%v", dms, err)
	}
	if dm, err := service.GetDirectConversation("dm_1"); err != nil || dm.ID != "dm_1" {
		t.Fatalf("unexpected dm %#v err=%v", dm, err)
	}
	if page, err := service.ListDMMessagesContext(context.Background(), "dm_1", protocol.PageRequest{Limit: 10}); err != nil || len(page.Messages) != 1 {
		t.Fatalf("unexpected dm page %#v err=%v", page, err)
	}
	if artifacts, err := service.ListArtifactsContext(context.Background(), protocol.ArtifactFilter{RoomID: "research"}, protocol.PageRequest{Limit: 10}); err != nil || len(artifacts.Artifacts) != 1 {
		t.Fatalf("unexpected artifacts %#v err=%v", artifacts, err)
	}
	if thread, err := service.GetThread("thread_1"); err != nil || thread.ID != "thread_1" {
		t.Fatalf("unexpected thread %#v err=%v", thread, err)
	}
	agent, err := service.GetAgent("alpha")
	if err != nil || agent.ID != "alpha" || len(agent.Rooms) != 1 {
		t.Fatalf("unexpected agent %#v err=%v", agent, err)
	}
	agents, err := service.ListAgentsContext(context.Background(), protocol.PageRequest{Limit: 1})
	if err != nil {
		t.Fatalf("ListAgentsContext() error = %v", err)
	}
	if len(agents.Agents) != 1 || !agents.Page.HasMore || agents.Page.NextAfter == "" {
		t.Fatalf("unexpected first agent page %#v", agents)
	}
	nextAgents, err := service.ListAgentsContext(context.Background(), protocol.PageRequest{
		After: agents.Page.NextAfter,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAgentsContext(after) error = %v", err)
	}
	if len(nextAgents.Agents) != 1 || nextAgents.Agents[0].ID != "gamma" {
		t.Fatalf("unexpected next agent page %#v", nextAgents)
	}
}
