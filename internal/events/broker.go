package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// Broker fans out events to live subscribers and replays recent history to
// resuming ones. A single mutex guards nextID, history, AND subscribers so
// that Publish and subscribeFrom are strictly serialized: an event is either
// captured in a new subscriber's replay snapshot (published before the
// subscribe critical section) or fanned out to it (published after) — never
// both (which would double-deliver and break the client's per-frame ACK) and
// never neither (the delivery gap this collapse closes). Do not reintroduce a
// second lock: the fan-out below runs inside the critical section and relies
// on being serialized against subscriber registration/removal.
type Broker struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]brokerSubscriber
	history     []protocol.Event
	delivery    attachmentDeliveryStore
}

type brokerSubscriber struct {
	events  chan protocol.Event
	durable bool
}

type attachmentDeliveryStore interface {
	PrepareAttachmentDeliveryContext(context.Context, string) ([]protocol.Event, error)
	AcknowledgeAttachmentDeliveryContext(context.Context, string, string) error
}

const brokerHistoryLimit = 256

func NewBroker(delivery ...attachmentDeliveryStore) *Broker {
	broker := &Broker{
		subscribers: make(map[uint64]brokerSubscriber),
		history:     make([]protocol.Event, 0, brokerHistoryLimit),
	}
	if len(delivery) > 0 {
		broker.delivery = delivery[0]
	}
	return broker
}

// SubscribeAgent establishes a persistent per-agent delivery baseline on the
// first attachment and replays every stored message event after the last ACK
// on later attachments. The broker lock makes replay registration atomic with
// live fan-out.
func (b *Broker) SubscribeAgent(ctx context.Context, agentID string) (<-chan protocol.Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.delivery == nil {
		return nil, fmt.Errorf("durable attachment delivery is not configured")
	}
	replay, err := b.delivery.PrepareAttachmentDeliveryContext(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return b.subscribeLocked(ctx, replay, true), nil
}

func (b *Broker) AcknowledgeAgent(ctx context.Context, agentID string, eventID string) error {
	if b.delivery == nil {
		return fmt.Errorf("durable attachment delivery is not configured")
	}
	return b.delivery.AcknowledgeAttachmentDeliveryContext(ctx, agentID, eventID)
}

func (b *Broker) DurableDeliveryConfigured() bool { return b.delivery != nil }

func (b *Broker) Subscribe(ctx context.Context) <-chan protocol.Event {
	return b.subscribeFrom(ctx, "")
}

func (b *Broker) SubscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event {
	return b.subscribeFrom(ctx, lastEventID)
}

func (b *Broker) Publish(event protocol.Event) {
	droppedCount := 0
	disconnectedDurable := 0

	b.mu.Lock()
	b.history = append(b.history, event)
	if len(b.history) > brokerHistoryLimit {
		copy(b.history, b.history[len(b.history)-brokerHistoryLimit:])
		b.history = b.history[:brokerHistoryLimit]
	}
	// Ordinary observers remain non-blocking and drop on overflow. Durable
	// attachment subscribers instead disconnect on overflow so their persisted
	// cursor replays the missed event. Logging is deferred until after unlock so
	// no arbitrary work runs under the lock.
	for id, subscriber := range b.subscribers {
		if subscriber.durable {
			// A full durable stream is closed instead of dropping or globally
			// blocking publishers. Its persisted cursor remains behind this event,
			// so the attachment reconnects and replays it from durable storage.
			select {
			case subscriber.events <- event:
			default:
				delete(b.subscribers, id)
				close(subscriber.events)
				disconnectedDurable++
			}
			continue
		}
		select {
		case subscriber.events <- event:
		default:
			droppedCount++
		}
	}
	b.mu.Unlock()

	for i := 0; i < droppedCount; i++ {
		observability.DefaultMetrics.RecordDroppedEvent()
	}
	if droppedCount > 0 {
		observability.Logger(context.Background(), "events.broker", "event_id", event.ID).
			Warn("drop event for slow subscriber")
	}
	if disconnectedDurable > 0 {
		observability.Logger(context.Background(), "events.broker", "event_id", event.ID).
			Warn("disconnect slow durable subscriber for replay", "subscriber_count", disconnectedDurable)
	}
}

func (b *Broker) subscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event {
	b.mu.Lock()
	replay := b.eventsAfterLocked(lastEventID)
	ch := b.subscribeLocked(ctx, replay, false)
	b.mu.Unlock()
	return ch
}

func (b *Broker) subscribeLocked(ctx context.Context, replay []protocol.Event, durable bool) <-chan protocol.Event {
	b.nextID++
	id := b.nextID

	// Buffer holds the full replay (len(replay) <= history <= brokerHistoryLimit)
	// plus headroom, so pushing replay below never blocks under the lock.
	bufferSize := brokerHistoryLimit
	if len(replay)+16 > bufferSize {
		bufferSize = len(replay) + 16
	}
	ch := make(chan protocol.Event, bufferSize)
	for _, event := range replay {
		ch <- event
	}
	// Snapshot-then-register in one critical section: Publish is serialized on
	// the same b.mu, so no event can be both replayed and fanned out, nor lost
	// between the snapshot and registration.
	b.subscribers[id] = brokerSubscriber{events: ch, durable: durable}

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()

		if subscriber, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(subscriber.events)
		}
	}()

	return ch
}

func (b *Broker) eventsAfterLocked(lastEventID string) []protocol.Event {
	if len(b.history) == 0 || lastEventID == "" {
		return nil
	}
	if strings.HasPrefix(lastEventID, "evt_gap_") {
		return append([]protocol.Event(nil), b.history...)
	}

	index := -1
	for i := len(b.history) - 1; i >= 0; i-- {
		if b.history[i].ID == lastEventID {
			index = i
			break
		}
	}

	if index == -1 {
		events := []protocol.Event{{
			ID:        replayGapEventID(lastEventID),
			Type:      protocol.EventTypeReplayGap,
			NetworkID: b.history[len(b.history)-1].NetworkID,
			ReplayGap: &protocol.ReplayGap{
				RequestedEventID: lastEventID,
				OldestEventID:    b.history[0].ID,
				NewestEventID:    b.history[len(b.history)-1].ID,
			},
			CreatedAt: time.Now().UTC(),
		}}
		return append(events, b.history...)
	}
	if index == len(b.history)-1 {
		return nil
	}

	return append([]protocol.Event(nil), b.history[index+1:]...)
}

func replayGapEventID(requestedEventID string) string {
	sum := sha256.Sum256([]byte(requestedEventID))
	return "evt_gap_" + hex.EncodeToString(sum[:])
}
