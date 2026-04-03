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

type failingHealthStore struct {
	*store.MemoryStore
	err error
}

func (s *failingHealthStore) Health(context.Context) error {
	return s.err
}

type subscribeOnlyBroker struct {
	ch chan protocol.Event
}

func (b *subscribeOnlyBroker) Publish(event protocol.Event) {
	select {
	case b.ch <- event:
	default:
	}
}

func (b *subscribeOnlyBroker) Subscribe(ctx context.Context) <-chan protocol.Event {
	go func() {
		<-ctx.Done()
		close(b.ch)
	}()
	return b.ch
}

func TestServiceGettersAndHealth(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "research",
		Members: []string{"alpha", "beta"},
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
		From: protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{
			{Kind: "text", Text: "thread reply"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_1",
			ParticipantIDs: []string{"alpha", "beta"},
		},
		From:  protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{{Kind: "text", Text: "ping"}},
	}); err != nil {
		t.Fatal(err)
	}

	room, err := service.GetRoom("research")
	if err != nil || room.ID != "research" {
		t.Fatalf("GetRoom() = %#v, %v", room, err)
	}

	thread, err := service.GetThread("thread_1")
	if err != nil || thread.ID != "thread_1" {
		t.Fatalf("GetThread() = %#v, %v", thread, err)
	}

	dm, err := service.GetDirectConversation("dm_1")
	if err != nil || dm.ID != "dm_1" {
		t.Fatalf("GetDirectConversation() = %#v, %v", dm, err)
	}

	agent, err := service.GetAgent("alpha")
	if err != nil || agent.ID != "alpha" {
		t.Fatalf("GetAgent() = %#v, %v", agent, err)
	}

	if err := service.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
}

func TestServiceGetterErrorsAndHealthPropagation(t *testing.T) {
	t.Parallel()

	memory := &failingHealthStore{
		MemoryStore: store.NewMemoryStore(),
		err:         errors.New("db unavailable"),
	}
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})

	if _, err := service.GetRoom("missing"); err == nil {
		t.Fatal("expected unknown room error")
	}
	if _, err := service.GetThread("missing"); err == nil {
		t.Fatal("expected unknown thread error")
	}
	if _, err := service.GetDirectConversation(""); err == nil {
		t.Fatal("expected invalid dm error")
	}
	if _, err := service.GetDirectConversation("missing"); err == nil {
		t.Fatal("expected unknown dm error")
	}
	if _, err := service.GetAgent("missing"); err == nil {
		t.Fatal("expected unknown agent error")
	}
	if err := service.Health(context.Background()); err == nil {
		t.Fatal("expected health error")
	}
}

func TestServiceSubscribeFromUsesReplayBrokerWhenAvailable(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}
	first, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: "text", Text: "one"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "beta"},
		Parts:  []protocol.Part{{Kind: "text", Text: "two"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := service.SubscribeFrom(ctx, first.EventID)
	select {
	case event := <-stream:
		if event.ID != second.EventID {
			t.Fatalf("unexpected replayed event %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replayed event")
	}
}

func TestServiceSubscribeFromFallsBackToLiveSubscribe(t *testing.T) {
	t.Parallel()

	broker := &subscribeOnlyBroker{ch: make(chan protocol.Event, 1)}
	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            broker,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := service.SubscribeFrom(ctx, "evt_missing")
	broker.Publish(protocol.Event{ID: "evt_live", Type: protocol.EventTypeMessageCreated})
	select {
	case event := <-stream:
		if event.ID != "evt_live" {
			t.Fatalf("unexpected live event %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live event")
	}
}
