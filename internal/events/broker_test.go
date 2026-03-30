package events

import (
	"context"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestBrokerPublishAndUnsubscribe(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	stream := broker.Subscribe(ctx)

	event := protocol.Event{ID: "evt_1", Type: protocol.EventTypeMessageCreated}
	broker.Publish(event)

	select {
	case received := <-stream:
		if received.ID != event.ID {
			t.Fatalf("unexpected event %#v", received)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	cancel()

	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("expected closed stream")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream close")
	}
}
