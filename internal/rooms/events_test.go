package rooms

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestEventIDForMessageIsStableAndCollisionSafe(t *testing.T) {
	t.Parallel()

	first := eventIDForMessage("msg/a")
	second := eventIDForMessage("msg_a")
	if first == second {
		t.Fatalf("expected distinct event ids, got %q", first)
	}
	if first == "" || second == "" {
		t.Fatalf("expected non-empty event ids, got %q and %q", first, second)
	}
	if len(first) > protocol.MaxMessageIDLength || len(second) > protocol.MaxMessageIDLength {
		t.Fatalf("expected cursor-safe event ids, got %d and %d characters", len(first), len(second))
	}
	if err := protocol.ValidateMessageID(first); err != nil {
		t.Fatalf("expected first event id to validate, got %v", err)
	}
	if err := protocol.ValidateMessageID(second); err != nil {
		t.Fatalf("expected second event id to validate, got %v", err)
	}
}

func TestEventIDForMessageStaysCursorSafeForLongMessageIDs(t *testing.T) {
	t.Parallel()

	messageID := strings.Repeat("m", protocol.MaxMessageIDLength)
	eventID := eventIDForMessage(messageID)
	if len(eventID) > protocol.MaxMessageIDLength {
		t.Fatalf("expected event id length <= %d, got %d", protocol.MaxMessageIDLength, len(eventID))
	}
	if err := protocol.ValidateMessageID(eventID); err != nil {
		t.Fatalf("expected long-message event id to validate, got %v", err)
	}
}

func TestAgentPresencePublishesEventsAndUpdatesAgentSummary(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.RegisterAgentContext(context.Background(), protocol.RegisterAgentRequest{
		RequestedAgentID: "luna",
		Name:             "Luna",
	}); err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}

	stream := service.Subscribe(context.Background())
	service.AgentConnected(context.Background(), protocol.Actor{ID: "luna", Name: "Luna"})

	connected := <-stream
	if connected.Type != protocol.EventTypeAgentConnected ||
		connected.Agent == nil ||
		connected.Agent.AgentID != "luna" {
		t.Fatalf("unexpected connected event %#v", connected)
	}

	agent, err := service.GetAgent("luna")
	if err != nil {
		t.Fatalf("GetAgent() error = %v", err)
	}
	if !agent.Connected {
		t.Fatalf("expected connected agent summary, got %#v", agent)
	}

	service.AgentDisconnected(context.Background(), protocol.Actor{ID: "luna", Name: "Luna"}, "", nil)
	disconnected := <-stream
	if disconnected.Type != protocol.EventTypeAgentDisconnected ||
		disconnected.Agent == nil ||
		disconnected.Agent.AgentID != "luna" {
		t.Fatalf("unexpected disconnected event %#v", disconnected)
	}

	agent, err = service.GetAgent("luna")
	if err != nil {
		t.Fatalf("GetAgent() after disconnect error = %v", err)
	}
	if agent.Connected {
		t.Fatalf("expected disconnected agent summary, got %#v", agent)
	}
}

func TestAgentPresenceDebugEventsIncludeLifecycleReasons(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{
		DebugEvents: true,
		NetworkID:   "local",
		NetworkName: "Local",
		Store:       nil,
		Messages:    nil,
		Broker:      events.NewBroker(),
	})

	stream := service.Subscribe(context.Background())
	service.AgentConnected(context.Background(), protocol.Actor{ID: "luna", Name: "Luna"})

	connected := <-stream
	if connected.Agent == nil || connected.Agent.Reason != "attachment_ready" || connected.Agent.Error != "" {
		t.Fatalf("unexpected debug connected event %#v", connected)
	}

	service.AgentDisconnected(
		context.Background(),
		protocol.Actor{ID: "luna", Name: "Luna"},
		"read error",
		context.Canceled,
	)
	disconnected := <-stream
	if disconnected.Agent == nil ||
		disconnected.Agent.Reason != "read_error" ||
		disconnected.Agent.Error != "" {
		t.Fatalf("unexpected debug disconnected event %#v", disconnected)
	}

	service.AgentDisconnected(
		context.Background(),
		protocol.Actor{ID: "luna", Name: "Luna"},
		"event_write_failed",
		errors.New("write tcp: broken pipe"),
	)
	failedDisconnect := <-stream
	if failedDisconnect.Agent == nil ||
		failedDisconnect.Agent.Reason != "event_write_failed" ||
		failedDisconnect.Agent.Error != "write tcp: broken pipe" {
		t.Fatalf("unexpected debug disconnect error event %#v", failedDisconnect)
	}
}

func TestAgentWakeEventsPublishMessageContext(t *testing.T) {
	t.Parallel()

	service := newTestService()
	stream := service.Subscribe(context.Background())
	messageEvent := protocol.Event{
		ID:        "evt_1",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "lab"},
			Mentions:  []string{"luna"},
		},
	}

	service.AgentWakeDelivered(context.Background(), protocol.Actor{ID: "luna", Name: "Luna"}, messageEvent)
	delivered := <-stream
	if delivered.Type != protocol.EventTypeAgentWakeDelivered ||
		delivered.Agent == nil ||
		delivered.Agent.MessageID != "msg_1" ||
		delivered.Agent.Reason != "mention" ||
		delivered.Agent.Target == nil ||
		delivered.Agent.Target.RoomID != "lab" {
		t.Fatalf("unexpected delivered event %#v", delivered)
	}

	service.AgentWakeFailed(context.Background(), protocol.Actor{ID: "luna", Name: "Luna"}, messageEvent, context.Canceled)
	failed := <-stream
	if failed.Type != protocol.EventTypeAgentWakeFailed ||
		failed.Agent == nil ||
		failed.Agent.Error != "context canceled" {
		t.Fatalf("unexpected failed event %#v", failed)
	}
}

type blockingEventBroker struct {
	published        chan protocol.Event
	blockFirst       chan struct{}
	firstPublishSeen chan struct{}
	count            int
}

func newBlockingEventBroker() *blockingEventBroker {
	return &blockingEventBroker{
		published:        make(chan protocol.Event, 4),
		blockFirst:       make(chan struct{}),
		firstPublishSeen: make(chan struct{}, 1),
	}
}

func (b *blockingEventBroker) Publish(event protocol.Event) {
	b.count++
	if b.count == 1 {
		b.firstPublishSeen <- struct{}{}
		<-b.blockFirst
	}
	b.published <- event
}

func (b *blockingEventBroker) Subscribe(ctx context.Context) <-chan protocol.Event {
	ch := make(chan protocol.Event)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch
}

func TestSetPairingStatusPublishesUpdatesInMutationOrder(t *testing.T) {
	t.Parallel()

	broker := newBlockingEventBroker()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Pairings: []protocol.Pairing{
			{ID: "pair_1", RemoteNetworkID: "remote", RemoteBaseURL: "https://remote.example.com", Status: "connected"},
		},
		Store:    nil,
		Messages: nil,
		Broker:   broker,
	})

	doneError := make(chan struct{})
	go func() {
		service.setPairingStatus("pair_1", "error")
		close(doneError)
	}()

	select {
	case <-broker.firstPublishSeen:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first pairing event")
	}

	doneConnected := make(chan struct{})
	go func() {
		service.setPairingStatus("pair_1", "connected")
		close(doneConnected)
	}()

	select {
	case event := <-broker.published:
		t.Fatalf("unexpected early pairing event %#v", event)
	case <-doneConnected:
		t.Fatal("expected second status update to block until first publish completes")
	case <-time.After(50 * time.Millisecond):
	}

	close(broker.blockFirst)

	select {
	case <-doneError:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first status update to complete")
	}
	select {
	case <-doneConnected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second status update to complete")
	}

	first := <-broker.published
	second := <-broker.published
	if first.Pairing == nil || second.Pairing == nil {
		t.Fatalf("expected pairing payloads, got first=%#v second=%#v", first, second)
	}
	if first.Pairing.Status != "error" || second.Pairing.Status != "connected" {
		t.Fatalf("expected ordered pairing statuses, got first=%#v second=%#v", first.Pairing, second.Pairing)
	}
}
