package events

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
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

func TestBrokerSubscribeFromReplaysBufferedEvents(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	broker.Publish(protocol.Event{ID: "evt_1", Type: protocol.EventTypeMessageCreated})
	broker.Publish(protocol.Event{ID: "evt_2", Type: protocol.EventTypeMessageCreated})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := broker.SubscribeFrom(ctx, "evt_1")

	select {
	case received := <-stream:
		if received.ID != "evt_2" {
			t.Fatalf("unexpected replayed event %#v", received)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replayed event")
	}
}

func TestBrokerSubscribeFromFallsBackToBufferedHistoryWhenCursorMissing(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	broker.Publish(protocol.Event{ID: "evt_1", Type: protocol.EventTypeMessageCreated})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := broker.SubscribeFrom(ctx, "evt_missing")

	select {
	case received := <-stream:
		if received.Type != protocol.EventTypeReplayGap || received.ReplayGap == nil {
			t.Fatalf("unexpected replayed event %#v", received)
		}
		if strings.Contains(received.ID, "evt_missing") {
			t.Fatalf("expected gap event id to be encoded, got %q", received.ID)
		}
		if len(received.ID) > protocol.MaxMessageIDLength {
			t.Fatalf("expected gap event id to fit cursor limits, got %q", received.ID)
		}
		if err := protocol.ValidateMessageID(received.ID); err != nil {
			t.Fatalf("expected valid gap event id, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replayed event")
	}

	select {
	case received := <-stream:
		if received.ID != "evt_1" {
			t.Fatalf("unexpected replayed event %#v", received)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replayed event")
	}
}

func TestBrokerSubscribeFromGapCursorReplaysBufferedHistoryWithoutRepeatingGap(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	broker.Publish(protocol.Event{ID: "evt_1", Type: protocol.EventTypeMessageCreated})
	broker.Publish(protocol.Event{ID: "evt_2", Type: protocol.EventTypeMessageCreated})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := broker.SubscribeFrom(ctx, "evt_missing")

	var gapEvent protocol.Event
	select {
	case gapEvent = <-initial:
		if gapEvent.Type != protocol.EventTypeReplayGap {
			t.Fatalf("unexpected gap event %#v", gapEvent)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial gap event")
	}

	replayed := broker.SubscribeFrom(ctx, gapEvent.ID)
	for _, wantID := range []string{"evt_1", "evt_2"} {
		select {
		case event := <-replayed:
			if event.Type == protocol.EventTypeReplayGap {
				t.Fatalf("unexpected repeated gap event %#v", event)
			}
			if event.ID != wantID {
				t.Fatalf("unexpected replayed event %#v, want %q", event, wantID)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for replayed event %q", wantID)
		}
	}
}

func TestBrokerRecordsDroppedEventsWhenSubscriberBufferFills(t *testing.T) {
	t.Parallel()

	previousMetrics := observability.DefaultMetrics
	observability.DefaultMetrics = observability.NewMetrics()
	t.Cleanup(func() {
		observability.DefaultMetrics = previousMetrics
	})

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = broker.Subscribe(ctx)
	for i := 0; i < brokerHistoryLimit+32; i++ {
		broker.Publish(protocol.Event{
			ID:   "evt_drop_" + time.Now().UTC().Add(time.Duration(i)*time.Millisecond).Format("150405.000000000"),
			Type: protocol.EventTypeMessageCreated,
		})
	}

	metrics := observability.DefaultMetrics.RenderPrometheus()
	if !strings.Contains(metrics, "moltnet_events_dropped_total ") {
		t.Fatalf("expected dropped event metric, got %s", metrics)
	}
	if strings.Contains(metrics, "moltnet_events_dropped_total 0") {
		t.Fatalf("expected dropped event count to increase, got %s", metrics)
	}
}

func TestBrokerReplayLeavesHeadroomForFirstLiveEvents(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	for index := 0; index < brokerHistoryLimit; index++ {
		broker.Publish(protocol.Event{
			ID:   "evt_history_" + time.Now().UTC().Add(time.Duration(index)*time.Millisecond).Format("150405.000000000"),
			Type: protocol.EventTypeMessageCreated,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := broker.SubscribeFrom(ctx, "evt_missing")
	broker.Publish(protocol.Event{ID: "evt_live", Type: protocol.EventTypeMessageCreated})

	seenLive := false
	deadline := time.After(time.Second)
	for received := 0; received < brokerHistoryLimit+2; received++ {
		select {
		case event := <-stream:
			if event.ID == "evt_live" {
				seenLive = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for replay and live events")
		}
	}

	if !seenLive {
		t.Fatal("expected first live event to survive replay buffering")
	}
}
