package rooms

import (
	"context"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

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
	if network.Capabilities.EventStream != "sse" || !network.Capabilities.HumanIngress || network.Capabilities.MessagePagination != "cursor" {
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

	rooms := service.ListRooms()
	if len(rooms) != 1 || rooms[0].ID != "research" {
		t.Fatalf("unexpected rooms %#v", rooms)
	}

	agents := service.ListAgents()
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

	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_1",
			ParticipantIDs: []string{"researcher", "writer"},
		},
		From:  protocol.Actor{Type: "agent", ID: "researcher"},
		Parts: []protocol.Part{{Kind: "text", Text: "ping"}},
	}); err != nil {
		t.Fatalf("SendMessage() dm error = %v", err)
	}

	conversations := service.ListDirectConversations()
	if len(conversations) != 1 || conversations[0].ID != "dm_1" {
		t.Fatalf("unexpected conversations %#v", conversations)
	}
	if len(conversations[0].ParticipantIDs) != 2 || conversations[0].ParticipantIDs[0] != "researcher" || conversations[0].ParticipantIDs[1] != "writer" {
		t.Fatalf("unexpected dm participants %#v", conversations[0].ParticipantIDs)
	}

	page, err := service.ListDMMessages("dm_1", "", 10)
	if err != nil {
		t.Fatalf("ListDMMessages() error = %v", err)
	}

	if len(page.Messages) != 1 || page.Messages[0].Parts[0].Text != "ping" {
		t.Fatalf("unexpected dm messages %#v", page)
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
			{ID: "pair_1", RemoteNetworkID: "remote", RemoteNetworkName: "Remote", Status: "connected"},
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

	pairings := service.ListPairings()
	if len(pairings) != 1 || pairings[0].RemoteNetworkID != "remote" {
		t.Fatalf("unexpected pairings %#v", pairings)
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

	firstPage, err := service.ListRoomMessages("research", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Messages) != 2 || firstPage.Messages[0].Parts[0].Text != "two" || !firstPage.Page.HasMore || firstPage.Page.NextBefore == "" {
		t.Fatalf("unexpected first page %#v", firstPage)
	}

	secondPage, err := service.ListRoomMessages("research", firstPage.Page.NextBefore, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Messages) != 1 || secondPage.Messages[0].Parts[0].Text != "one" || secondPage.Page.HasMore {
		t.Fatalf("unexpected second page %#v", secondPage)
	}
}
