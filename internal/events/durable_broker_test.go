package events

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestDurableAgentReplaySurvivesRestartAndACKIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moltnet.db")
	first := openDurableTestStore(t, path)
	registerDurableTestAgent(t, first, "alpha")
	registerDurableTestAgent(t, first, "beta")

	baselineCtx, cancelBaseline := context.WithCancel(context.Background())
	stream, err := NewBroker(first).SubscribeAgent(baselineCtx, "alpha")
	if err != nil {
		t.Fatalf("establish alpha baseline: %v", err)
	}
	requireNoDurableEvent(t, stream)
	cancelBaseline()

	firstEvent := durableTestEvent("msg_1", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if _, err := first.AppendMessageEventWithLifecycleContext(t.Context(), *firstEvent.Message, firstEvent); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	second := openDurableTestStore(t, path)
	replayCtx, cancelReplay := context.WithCancel(context.Background())
	secondBroker := NewBroker(second)
	replay, err := secondBroker.SubscribeAgent(replayCtx, "alpha")
	if err != nil {
		t.Fatalf("replay alpha after restart: %v", err)
	}
	if event := requireDurableEvent(t, replay); event.ID != firstEvent.ID {
		t.Fatalf("replayed event = %q, want %q", event.ID, firstEvent.ID)
	}
	if err := secondBroker.AcknowledgeAgent(t.Context(), "alpha", firstEvent.ID); err != nil {
		t.Fatalf("ack first event: %v", err)
	}
	cancelReplay()

	secondEvent := durableTestEvent("msg_2", time.Date(2026, 8, 24, 12, 1, 0, 0, time.UTC))
	if _, err := second.AppendMessageEventWithLifecycleContext(t.Context(), *secondEvent.Message, secondEvent); err != nil {
		t.Fatalf("append second event: %v", err)
	}
	thirdCtx, cancelThird := context.WithCancel(context.Background())
	thirdReplay, err := secondBroker.SubscribeAgent(thirdCtx, "alpha")
	if err != nil {
		t.Fatalf("replay second event: %v", err)
	}
	if event := requireDurableEvent(t, thirdReplay); event.ID != secondEvent.ID {
		t.Fatalf("replayed event = %q, want %q", event.ID, secondEvent.ID)
	}
	if err := secondBroker.AcknowledgeAgent(t.Context(), "alpha", secondEvent.ID); err != nil {
		t.Fatalf("ack second event: %v", err)
	}
	// Duplicate and older ACKs cannot regress the durable cursor.
	if err := secondBroker.AcknowledgeAgent(t.Context(), "alpha", secondEvent.ID); err != nil {
		t.Fatalf("duplicate ack: %v", err)
	}
	if err := secondBroker.AcknowledgeAgent(t.Context(), "alpha", firstEvent.ID); err != nil {
		t.Fatalf("older ack: %v", err)
	}
	cancelThird()
	if err := second.Close(); err != nil {
		t.Fatalf("close second store: %v", err)
	}

	third := openDurableTestStore(t, path)
	t.Cleanup(func() { _ = third.Close() })
	ackedCtx, cancelAcked := context.WithCancel(context.Background())
	acked, err := NewBroker(third).SubscribeAgent(ackedCtx, "alpha")
	if err != nil {
		t.Fatalf("reconnect acknowledged alpha: %v", err)
	}
	requireNoDurableEvent(t, acked)
	cancelAcked()

	// Beta existed before both messages but never attached. Its genuinely new
	// empty-cursor attachment starts at the current durable head.
	betaCtx, cancelBeta := context.WithCancel(context.Background())
	beta, err := NewBroker(third).SubscribeAgent(betaCtx, "beta")
	if err != nil {
		t.Fatalf("establish beta baseline: %v", err)
	}
	requireNoDurableEvent(t, beta)
	cancelBeta()
}

func TestDurableSubscriberDisconnectsForReplayInsteadOfDropping(t *testing.T) {
	memory := store.NewMemoryStore()
	broker := NewBroker(memory)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := broker.SubscribeAgent(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	var overflow protocol.Event
	for index := 0; index <= brokerHistoryLimit; index++ {
		event := durableTestEvent(fmt.Sprintf("msg_buffered_%03d", index), time.Unix(int64(index), 0).UTC())
		if _, err := memory.AppendMessageEventWithLifecycleContext(t.Context(), *event.Message, event); err != nil {
			t.Fatalf("append buffered event %d: %v", index, err)
		}
		broker.Publish(event)
		overflow = event
	}
	received := 0
	for range stream {
		received++
	}
	if received != brokerHistoryLimit {
		t.Fatalf("events before durable disconnect = %d, want %d", received, brokerHistoryLimit)
	}
	cancel()

	replayCtx, cancelReplay := context.WithCancel(context.Background())
	defer cancelReplay()
	replay, err := broker.SubscribeAgent(replayCtx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	var last protocol.Event
	for index := 0; index <= brokerHistoryLimit; index++ {
		last = requireDurableEvent(t, replay)
	}
	if last.ID != overflow.ID {
		t.Fatalf("last replay = %q, want overflow event %q", last.ID, overflow.ID)
	}
}

func openDurableTestStore(t *testing.T, path string) *store.SQLStore {
	t.Helper()
	opened, err := store.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	return opened
}

func registerDurableTestAgent(t *testing.T, target *store.SQLStore, agentID string) {
	t.Helper()
	now := time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC)
	_, err := target.RegisterAgentContext(t.Context(), protocol.AgentRegistration{
		NetworkID: "local", AgentID: agentID, ActorUID: "actor_" + agentID,
		ActorURI: protocol.AgentFQID("local", agentID), CredentialKey: "credential_" + agentID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("register agent %s: %v", agentID, err)
	}
}

func durableTestEvent(messageID string, createdAt time.Time) protocol.Event {
	message := protocol.Message{
		ID: messageID, NetworkID: "local", From: protocol.Actor{Type: "agent", ID: "writer"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}}, CreatedAt: createdAt,
	}
	return protocol.Event{
		ID: protocol.MessageEventID(messageID), Type: protocol.EventTypeMessageCreated,
		NetworkID: "local", Message: &message, CreatedAt: createdAt,
	}
}

func requireDurableEvent(t *testing.T, stream <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case event := <-stream:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for durable event")
		return protocol.Event{}
	}
}

func requireNoDurableEvent(t *testing.T, stream <-chan protocol.Event) {
	t.Helper()
	select {
	case event := <-stream:
		t.Fatalf("unexpected durable event %#v", event)
	case <-time.After(25 * time.Millisecond):
	}
}
