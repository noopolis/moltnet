package rooms

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSendMessageThreadLifecycleFlagsStayExplicitAcrossDuplicates(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	request := protocol.SendMessageRequest{
		ID: "msg_thread_duplicate",
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_duplicate",
			ParentMessageID: "msg_parent",
		},
		From:  protocol.Actor{Type: "agent", ID: "writer"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}

	first, err := service.SendMessage(request)
	if err != nil {
		t.Fatalf("first SendMessage() error = %v", err)
	}
	second, err := service.SendMessage(request)
	if err != nil {
		t.Fatalf("second SendMessage() error = %v", err)
	}

	if !first.ThreadCreated || first.DMCreated {
		t.Fatalf("unexpected first thread flags %#v", first)
	}
	if second.ThreadCreated || second.DMCreated {
		t.Fatalf("expected duplicate thread request to report no new lifecycle side effects, got %#v", second)
	}
	if first.EventID == "" || first.EventID != second.EventID {
		t.Fatalf("expected stable event id, got first=%#v second=%#v", first, second)
	}
}

func TestSendMessageDMLifecycleFlagsStayExplicitAcrossDuplicates(t *testing.T) {
	t.Parallel()

	service := newTestService()

	request := protocol.SendMessageRequest{
		ID: "msg_dm_duplicate",
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_duplicate",
			ParticipantIDs: []string{"alpha", "beta"},
		},
		From:  protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "ping"}},
	}

	first, err := service.SendMessage(request)
	if err != nil {
		t.Fatalf("first SendMessage() error = %v", err)
	}
	second, err := service.SendMessage(request)
	if err != nil {
		t.Fatalf("second SendMessage() error = %v", err)
	}

	if first.ThreadCreated || !first.DMCreated {
		t.Fatalf("unexpected first dm flags %#v", first)
	}
	if second.ThreadCreated || second.DMCreated {
		t.Fatalf("expected duplicate dm request to report no new lifecycle side effects, got %#v", second)
	}
	if first.EventID == "" || first.EventID != second.EventID {
		t.Fatalf("expected stable event id, got first=%#v second=%#v", first, second)
	}
}

type failingLifecycleStore struct {
	*store.MemoryStore
	threadErr error
	dmErr     error
}

type noLifecycleMessageStore struct {
	memory *store.MemoryStore
}

func (s *noLifecycleMessageStore) AppendMessage(message protocol.Message) error {
	return s.memory.AppendMessage(message)
}

func (s *noLifecycleMessageStore) AppendMessageContext(ctx context.Context, message protocol.Message) error {
	return s.memory.AppendMessageContext(ctx, message)
}

func (s *noLifecycleMessageStore) ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error) {
	return s.memory.ListRoomMessages(roomID, before, limit)
}

func (s *noLifecycleMessageStore) ListRoomMessagesContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	return s.memory.ListRoomMessagesContext(ctx, roomID, page)
}

func (s *noLifecycleMessageStore) ListThreads(roomID string) ([]protocol.Thread, error) {
	return s.memory.ListThreads(roomID)
}

func (s *noLifecycleMessageStore) ListThreadsContext(ctx context.Context, roomID string) ([]protocol.Thread, error) {
	return s.memory.ListThreadsContext(ctx, roomID)
}

func (s *noLifecycleMessageStore) ListThreadMessages(threadID string, before string, limit int) (protocol.MessagePage, error) {
	return s.memory.ListThreadMessages(threadID, before, limit)
}

func (s *noLifecycleMessageStore) ListThreadMessagesContext(ctx context.Context, threadID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	return s.memory.ListThreadMessagesContext(ctx, threadID, page)
}

func (s *noLifecycleMessageStore) ListDirectConversations() ([]protocol.DirectConversation, error) {
	return s.memory.ListDirectConversations()
}

func (s *noLifecycleMessageStore) ListDirectConversationsContext(ctx context.Context) ([]protocol.DirectConversation, error) {
	return s.memory.ListDirectConversationsContext(ctx)
}

func (s *noLifecycleMessageStore) GetDirectConversationContext(ctx context.Context, dmID string) (protocol.DirectConversation, bool, error) {
	return s.memory.GetDirectConversationContext(ctx, dmID)
}

func (s *noLifecycleMessageStore) ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error) {
	return s.memory.ListDMMessages(dmID, before, limit)
}

func (s *noLifecycleMessageStore) ListDMMessagesContext(ctx context.Context, dmID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	return s.memory.ListDMMessagesContext(ctx, dmID, page)
}

func (s *noLifecycleMessageStore) ListArtifacts(filter protocol.ArtifactFilter, before string, limit int) (protocol.ArtifactPage, error) {
	return s.memory.ListArtifacts(filter, before, limit)
}

func (s *noLifecycleMessageStore) ListArtifactsContext(ctx context.Context, filter protocol.ArtifactFilter, page protocol.PageRequest) (protocol.ArtifactPage, error) {
	return s.memory.ListArtifactsContext(ctx, filter, page)
}

func (s *failingLifecycleStore) AppendMessageWithLifecycleContext(
	ctx context.Context,
	message protocol.Message,
) (store.AppendLifecycle, error) {
	if s.threadErr != nil {
		return store.AppendLifecycle{}, s.threadErr
	}
	if s.dmErr != nil {
		return store.AppendLifecycle{}, s.dmErr
	}
	return s.MemoryStore.AppendMessageWithLifecycleContext(ctx, message)
}

func TestSendMessageReturnsLifecycleLookupErrors(t *testing.T) {
	t.Parallel()

	base := store.NewMemoryStore()
	failing := &failingLifecycleStore{
		MemoryStore: base,
		threadErr:   errors.New("thread lookup failed"),
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
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	_, err := service.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_failure",
			ParentMessageID: "msg_parent",
		},
		From:  protocol.Actor{Type: "agent", ID: "writer"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "thread lookup failed") {
		t.Fatalf("expected lifecycle lookup error, got %v", err)
	}
}

func TestSendMessageFallbackLifecyclePath(t *testing.T) {
	t.Parallel()

	base := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             base,
		Messages:          &noLifecycleMessageStore{memory: base},
		Broker:            events.NewBroker(),
	})
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	threadAccepted, err := service.SendMessage(protocol.SendMessageRequest{
		ID: "msg_thread_fallback",
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          "research",
			ThreadID:        "thread_fallback",
			ParentMessageID: "msg_parent",
		},
		From:  protocol.Actor{Type: "agent", ID: "writer"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("SendMessage(thread fallback) error = %v", err)
	}
	if !threadAccepted.ThreadCreated || threadAccepted.DMCreated {
		t.Fatalf("unexpected thread fallback flags %#v", threadAccepted)
	}

	dmAccepted, err := service.SendMessage(protocol.SendMessageRequest{
		ID: "msg_dm_fallback",
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_fallback",
			ParticipantIDs: []string{"alpha", "beta"},
		},
		From:  protocol.Actor{Type: "agent", ID: "alpha"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "ping"}},
	})
	if err != nil {
		t.Fatalf("SendMessage(dm fallback) error = %v", err)
	}
	if dmAccepted.ThreadCreated || !dmAccepted.DMCreated {
		t.Fatalf("unexpected dm fallback flags %#v", dmAccepted)
	}
}

func TestSendMessagePublishesSingleThreadCreatedEventUnderConcurrency(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := service.Subscribe(ctx)

	start := make(chan struct{})
	var wg sync.WaitGroup
	send := func(id string) {
		defer wg.Done()
		<-start
		if _, err := service.SendMessage(protocol.SendMessageRequest{
			ID: id,
			Target: protocol.Target{
				Kind:            protocol.TargetKindThread,
				RoomID:          "research",
				ThreadID:        "thread_concurrent",
				ParentMessageID: "msg_parent",
			},
			From:  protocol.Actor{Type: "agent", ID: id},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: id}},
		}); err != nil {
			t.Errorf("SendMessage(%s) error = %v", id, err)
		}
	}

	wg.Add(2)
	go send("alpha")
	go send("beta")
	close(start)
	wg.Wait()

	threadCreated := 0
	deadline := time.After(time.Second)
	for threadCreated < 1 {
		select {
		case event := <-stream:
			if event.Type == protocol.EventTypeThreadCreated {
				threadCreated++
			}
		case <-deadline:
			t.Fatalf("timed out waiting for thread lifecycle event, got %d", threadCreated)
		}
	}

	select {
	case event := <-stream:
		if event.Type == protocol.EventTypeThreadCreated {
			t.Fatalf("expected a single thread.created event, got another %#v", event)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSendMessagePublishesSingleDMCreatedEventUnderConcurrency(t *testing.T) {
	t.Parallel()

	service := newTestService()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := service.Subscribe(ctx)

	start := make(chan struct{})
	var wg sync.WaitGroup
	send := func(id string) {
		defer wg.Done()
		<-start
		if _, err := service.SendMessage(protocol.SendMessageRequest{
			ID: "msg_" + id,
			Target: protocol.Target{
				Kind:           protocol.TargetKindDM,
				DMID:           "dm_concurrent",
				ParticipantIDs: []string{"alpha", "beta"},
			},
			From:  protocol.Actor{Type: "agent", ID: id},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: id}},
		}); err != nil {
			t.Errorf("SendMessage(%s) error = %v", id, err)
		}
	}

	wg.Add(2)
	go send("alpha")
	go send("beta")
	close(start)
	wg.Wait()

	dmCreated := 0
	deadline := time.After(time.Second)
	for dmCreated < 1 {
		select {
		case event := <-stream:
			if event.Type == protocol.EventTypeDMCreated {
				dmCreated++
			}
		case <-deadline:
			t.Fatalf("timed out waiting for dm lifecycle event, got %d", dmCreated)
		}
	}

	select {
	case event := <-stream:
		if event.Type == protocol.EventTypeDMCreated {
			t.Fatalf("expected a single dm.created event, got another %#v", event)
		}
	case <-time.After(50 * time.Millisecond):
	}
}
