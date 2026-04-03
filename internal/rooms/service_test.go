package rooms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type failingListStore struct {
	*store.MemoryStore
	listRoomsErr         error
	listConversationsErr error
}

func (s *failingListStore) ListRoomsContext(context.Context) ([]protocol.Room, error) {
	if s.listRoomsErr != nil {
		return nil, s.listRoomsErr
	}
	return s.MemoryStore.ListRoomsContext(context.Background())
}

func (s *failingListStore) ListDirectConversationsContext(context.Context) ([]protocol.DirectConversation, error) {
	if s.listConversationsErr != nil {
		return nil, s.listConversationsErr
	}
	return s.MemoryStore.ListDirectConversationsContext(context.Background())
}

func (s *failingListStore) ListAgentsContext(context.Context) ([]protocol.AgentSummary, error) {
	if s.listRoomsErr != nil {
		return nil, s.listRoomsErr
	}
	return s.MemoryStore.ListAgentsContext(context.Background())
}

func newTestService() *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})
}

func TestServiceCreateRoomAndNetwork(t *testing.T) {
	t.Parallel()

	service := newTestService()
	network := service.Network()
	if network.ID != "local" || network.Name != "Local" || network.Version != "test" {
		t.Fatalf("unexpected network %#v", network)
	}
	if network.Capabilities.EventStream != "sse" ||
		network.Capabilities.AttachmentProtocol != "websocket" ||
		!network.Capabilities.HumanIngress ||
		network.Capabilities.MessagePagination != "cursor" {
		t.Fatalf("unexpected network capabilities %#v", network.Capabilities)
	}

	room, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "research",
		Members: []string{"orchestrator", "researcher"},
	})
	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	if room.Name != "research" {
		t.Fatalf("expected default name, got %q", room.Name)
	}
	if room.FQID != "molt://local/rooms/research" {
		t.Fatalf("unexpected room fqid %q", room.FQID)
	}
	if room.NetworkID != "local" {
		t.Fatalf("unexpected room network id %q", room.NetworkID)
	}

	rooms, err := service.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms() error = %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID != "research" {
		t.Fatalf("unexpected rooms %#v", rooms)
	}

	agents, err := service.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(agents) != 2 || agents[0].FQID == "" || agents[0].NetworkID != "local" {
		t.Fatalf("unexpected agents %#v", agents)
	}
}

func TestServiceCreateRoomValidation(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{}); err == nil {
		t.Fatal("expected missing room id error")
	}
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "bad room"}); err == nil {
		t.Fatal("expected invalid room id error")
	}
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research", Members: []string{"bad\nmember"}}); err == nil {
		t.Fatal("expected invalid room member error")
	}
	room, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "research",
		Members: []string{" writer ", "writer", "reviewer"},
	})
	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	if len(room.Members) != 2 || room.Members[0] != "reviewer" || room.Members[1] != "writer" {
		t.Fatalf("unexpected deduplicated members %#v", room.Members)
	}
}

func TestServiceListWrappersReturnUnderlyingErrors(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	failing := &failingListStore{
		MemoryStore:          memory,
		listRoomsErr:         errors.New("list rooms failed"),
		listConversationsErr: errors.New("list conversations failed"),
	}
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             failing,
		Messages:          failing,
		Broker:            events.NewBroker(),
	})

	if _, err := service.ListRooms(); err == nil || err.Error() != "list rooms failed" {
		t.Fatalf("expected list rooms error, got %v", err)
	}
	if _, err := service.ListAgents(); err == nil || err.Error() != "list rooms failed" {
		t.Fatalf("expected list agents error, got %v", err)
	}
	if _, err := service.ListDirectConversations(); err == nil || err.Error() != "list conversations failed" {
		t.Fatalf("expected list conversations error, got %v", err)
	}
}

func TestServiceSendMessageAndHistory(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	subscribeCtx, cancelSubscribe := context.WithCancel(context.Background())
	defer cancelSubscribe()
	stream := service.Subscribe(subscribeCtx)

	accepted, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "orchestrator"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	if !accepted.Accepted || accepted.MessageID == "" || accepted.EventID == "" {
		t.Fatalf("unexpected acceptance %#v", accepted)
	}
	if accepted.ThreadCreated || accepted.DMCreated {
		t.Fatalf("unexpected conversation creation flags %#v", accepted)
	}

	select {
	case event := <-stream:
		if event.Message == nil || event.Message.ID != accepted.MessageID {
			t.Fatalf("unexpected event %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	page, err := service.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}

	if len(page.Messages) != 1 || page.Messages[0].Parts[0].Text != "hello" {
		t.Fatalf("unexpected messages %#v", page)
	}
}

func TestServiceSendMessageNormalizesMentions(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	accepted, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "human", ID: "tester", Name: "Tester"},
		Parts: []protocol.Part{
			{Kind: "text", Text: "@orchestrator please ask @researcher for one detail"},
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	page, err := service.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}

	if len(page.Messages) != 1 || page.Messages[0].ID != accepted.MessageID {
		t.Fatalf("unexpected messages %#v", page)
	}

	if len(page.Messages[0].Mentions) != 2 || page.Messages[0].Mentions[0] != "orchestrator" || page.Messages[0].Mentions[1] != "researcher" {
		t.Fatalf("unexpected mentions %#v", page.Messages[0].Mentions)
	}
}

func TestServiceDirectMessagesAndErrors(t *testing.T) {
	t.Parallel()

	service := newTestService()

	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "missing"},
		From:   protocol.Actor{Type: "agent", ID: "orchestrator"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	}); err == nil {
		t.Fatal("expected missing room error")
	}

	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: "bad"},
		From:   protocol.Actor{Type: "agent", ID: "orchestrator"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	}); err == nil {
		t.Fatal("expected invalid target error")
	}

	if _, err := service.ListRoomMessages("missing", "", 10); err == nil {
		t.Fatal("expected unknown room error")
	}

	if _, err := service.ListDMMessages("", "", 10); err == nil {
		t.Fatal("expected missing dm id error")
	}
	if _, err := service.ListArtifactsContext(context.Background(), protocol.ArtifactFilter{}, protocol.PageRequest{Limit: 10}); err == nil {
		t.Fatal("expected unscoped artifacts error")
	}

	accepted, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_1",
			ParticipantIDs: []string{"researcher", "writer"},
		},
		From:  protocol.Actor{Type: "agent", ID: "researcher"},
		Parts: []protocol.Part{{Kind: "text", Text: "ping"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() dm error = %v", err)
	}
	if !accepted.DMCreated || accepted.ThreadCreated {
		t.Fatalf("unexpected dm acceptance %#v", accepted)
	}

	conversations, err := service.ListDirectConversations()
	if err != nil {
		t.Fatalf("ListDirectConversations() error = %v", err)
	}
	if len(conversations.DMs) != 1 || conversations.DMs[0].ID != "dm_1" {
		t.Fatalf("unexpected conversations %#v", conversations)
	}
	if len(conversations.DMs[0].ParticipantIDs) != 2 || conversations.DMs[0].ParticipantIDs[0] != "researcher" || conversations.DMs[0].ParticipantIDs[1] != "writer" {
		t.Fatalf("unexpected dm participants %#v", conversations.DMs[0].ParticipantIDs)
	}

	page, err := service.ListDMMessages("dm_1", "", 10)
	if err != nil {
		t.Fatalf("ListDMMessages() error = %v", err)
	}

	if len(page.Messages) != 1 || page.Messages[0].Parts[0].Text != "ping" {
		t.Fatalf("unexpected dm messages %#v", page)
	}

	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_2",
			ParticipantIDs: []string{"researcher", "writer"},
		},
		From:  protocol.Actor{Type: "agent", ID: "researcher"},
		Parts: []protocol.Part{{Kind: "mystery", Text: "ping"}},
	}); err == nil {
		t.Fatal("expected unknown part kind error")
	}
}

func TestServiceThreadsAndArtifacts(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	accepted, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_1",
			ParentMessageID: "msg_parent",
		},
		From: protocol.Actor{Type: "agent", ID: "writer"},
		Parts: []protocol.Part{
			{Kind: "text", Text: "reply"},
			{Kind: "image", URL: "https://example.com/mock.png", Filename: "mock.png"},
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() thread error = %v", err)
	}
	if !accepted.ThreadCreated || accepted.DMCreated {
		t.Fatalf("unexpected thread acceptance %#v", accepted)
	}

	threads, err := service.ListThreads("research")
	if err != nil {
		t.Fatalf("ListThreads() error = %v", err)
	}
	if len(threads.Threads) != 1 || threads.Threads[0].ID != "thread_1" || threads.Threads[0].ParentMessageID != "msg_parent" {
		t.Fatalf("unexpected threads %#v", threads)
	}

	threadPage, err := service.ListThreadMessages("thread_1", "", 10)
	if err != nil {
		t.Fatalf("ListThreadMessages() error = %v", err)
	}
	if len(threadPage.Messages) != 1 || threadPage.Messages[0].Target.RoomID != "research" {
		t.Fatalf("unexpected thread page %#v", threadPage)
	}

	artifactPage, err := service.ListArtifacts(protocol.ArtifactFilter{ThreadID: "thread_1"}, "", 10)
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(artifactPage.Artifacts) != 1 || artifactPage.Artifacts[0].Filename != "mock.png" {
		t.Fatalf("unexpected artifacts %#v", artifactPage)
	}
}

func TestServiceListThreadsAndDirectConversationsPagination(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	for _, threadID := range []string{"thread_1", "thread_2", "thread_3"} {
		if _, err := service.SendMessage(protocol.SendMessageRequest{
			Target: protocol.Target{
				Kind:            protocol.TargetKindThread,
				RoomID:          "research",
				ThreadID:        threadID,
				ParentMessageID: "msg_parent",
			},
			From:  protocol.Actor{Type: "agent", ID: "writer"},
			Parts: []protocol.Part{{Kind: "text", Text: threadID}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	for _, dmID := range []string{"dm_1", "dm_2", "dm_3"} {
		if _, err := service.SendMessage(protocol.SendMessageRequest{
			Target: protocol.Target{
				Kind:           protocol.TargetKindDM,
				DMID:           dmID,
				ParticipantIDs: []string{"alpha", "beta"},
			},
			From:  protocol.Actor{Type: "agent", ID: "alpha"},
			Parts: []protocol.Part{{Kind: "text", Text: dmID}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	threadPage, err := service.ListThreadsContext(context.Background(), "research", protocol.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(threadPage.Threads) != 2 || !threadPage.Page.HasMore || threadPage.Page.NextAfter == "" {
		t.Fatalf("unexpected thread page %#v", threadPage)
	}

	threadAfterPage, err := service.ListThreadsContext(context.Background(), "research", protocol.PageRequest{
		After: threadPage.Page.NextAfter,
		Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(threadAfterPage.Threads) != 1 || threadAfterPage.Threads[0].ID != "thread_3" {
		t.Fatalf("unexpected thread after page %#v", threadAfterPage)
	}

	dmPage, err := service.ListDirectConversationsContext(context.Background(), protocol.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(dmPage.DMs) != 2 || !dmPage.Page.HasMore || dmPage.Page.NextAfter == "" {
		t.Fatalf("unexpected dm page %#v", dmPage)
	}

	dmAfterPage, err := service.ListDirectConversationsContext(context.Background(), protocol.PageRequest{
		After: dmPage.Page.NextAfter,
		Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dmAfterPage.DMs) != 1 || dmAfterPage.DMs[0].ID != "dm_3" {
		t.Fatalf("unexpected dm after page %#v", dmAfterPage)
	}
}

func TestServiceUpdateRoomMembersNoOpDoesNotEmitEvent(t *testing.T) {
	t.Parallel()

	service := newTestService()
	room, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "research",
		Members: []string{"alpha"},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := service.Subscribe(ctx)

	updated, err := service.UpdateRoomMembers(context.Background(), room.ID, protocol.UpdateRoomMembersRequest{
		Add:    []string{"alpha"},
		Remove: []string{"missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !membersEqual(updated.Members, room.Members) {
		t.Fatalf("expected unchanged members, got %#v", updated.Members)
	}

	select {
	case event := <-stream:
		t.Fatalf("unexpected event %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestServiceRejectsHumanIngressWhenDisabledAndListsPairings(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: false,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Pairings: []protocol.Pairing{
			{ID: "pair_1", RemoteNetworkID: "remote", RemoteNetworkName: "Remote", Status: "connected", Token: "secret"},
		},
		Store:    memory,
		Messages: memory,
		Broker:   events.NewBroker(),
	})

	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "human", ID: "tester"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	}); err == nil {
		t.Fatal("expected human ingress rejection")
	}

	network := service.Network()
	if network.Capabilities.HumanIngress {
		t.Fatalf("expected human ingress disabled, got %#v", network.Capabilities)
	}
	if !network.Capabilities.Pairings {
		t.Fatalf("expected pairings capability, got %#v", network.Capabilities)
	}

	pairings, err := service.ListPairings()
	if err != nil {
		t.Fatalf("ListPairings() error = %v", err)
	}
	if len(pairings) != 1 || pairings[0].RemoteNetworkID != "remote" {
		t.Fatalf("unexpected pairings %#v", pairings)
	}
	if pairings[0].Token != "" {
		t.Fatalf("expected pairing token to be hidden, got %#v", pairings[0])
	}

	pairingPage, err := service.ListPairingsContext(context.Background(), protocol.PageRequest{
		After: "missing",
		Limit: 1,
	})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got page=%#v err=%v", pairingPage, err)
	}
}

func TestServiceAcceptsDuplicateMessageIDIdempotently(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	request := protocol.SendMessageRequest{
		ID:     "msg_dedup_1",
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "orchestrator"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	}
	first, err := service.SendMessage(request)
	if err != nil {
		t.Fatalf("first SendMessage() error = %v", err)
	}
	second, err := service.SendMessage(request)
	if err != nil {
		t.Fatalf("second SendMessage() error = %v", err)
	}

	if first.MessageID != "msg_dedup_1" || second.MessageID != "msg_dedup_1" || !second.Accepted {
		t.Fatalf("unexpected duplicate acceptance first=%#v second=%#v", first, second)
	}
	if second.EventID != first.EventID || second.EventID == "" {
		t.Fatalf("expected duplicate acceptance with stable event id, got %#v", second)
	}

	page, err := service.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 1 {
		t.Fatalf("expected one stored message after duplicate send, got %#v", page)
	}
}

func TestServicePaginationRejectsUnknownCursorAcrossCollections(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "research",
		Members: []string{"alpha", "beta"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "ops",
		Members: []string{"gamma"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_1",
			ParentMessageID: "msg_parent",
		},
		From:  protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "thread"}},
	}); err != nil {
		t.Fatalf("SendMessage(thread) error = %v", err)
	}

	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_1",
			ParticipantIDs: []string{"alpha", "beta"},
		},
		From:  protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "dm"}},
	}); err != nil {
		t.Fatalf("SendMessage(dm) error = %v", err)
	}

	for _, test := range []struct {
		name string
		run  func() error
	}{
		{
			name: "rooms",
			run: func() error {
				_, err := service.ListRoomsContext(context.Background(), protocol.PageRequest{After: "missing", Limit: 1})
				return err
			},
		},
		{
			name: "threads",
			run: func() error {
				_, err := service.ListThreadsContext(context.Background(), "research", protocol.PageRequest{After: "missing", Limit: 1})
				return err
			},
		},
		{
			name: "dms",
			run: func() error {
				_, err := service.ListDirectConversationsContext(context.Background(), protocol.PageRequest{After: "missing", Limit: 1})
				return err
			},
		},
		{
			name: "agents",
			run: func() error {
				_, err := service.ListAgentsContext(context.Background(), protocol.PageRequest{After: "missing", Limit: 1})
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("expected ErrInvalidCursor, got %v", err)
			}
		})
	}
}

func TestServiceMessagePagination(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{"one", "two", "three"} {
		if _, err := service.SendMessage(protocol.SendMessageRequest{
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:   protocol.Actor{Type: "agent", ID: "orchestrator"},
			Parts:  []protocol.Part{{Kind: "text", Text: text}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	firstPage, err := service.ListRoomMessagesContext(context.Background(), "research", protocol.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Messages) != 2 || firstPage.Messages[0].Parts[0].Text != "two" || !firstPage.Page.HasMore || firstPage.Page.NextBefore == "" {
		t.Fatalf("unexpected first page %#v", firstPage)
	}

	secondPage, err := service.ListRoomMessagesContext(context.Background(), "research", protocol.PageRequest{
		Before: firstPage.Page.NextBefore,
		Limit:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Messages) != 1 || secondPage.Messages[0].Parts[0].Text != "one" || secondPage.Page.HasMore {
		t.Fatalf("unexpected second page %#v", secondPage)
	}
}
