package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	subscribers map[uint64]chan protocol.Event
	history     []protocol.Event
}

const brokerHistoryLimit = 256

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[uint64]chan protocol.Event),
		history:     make([]protocol.Event, 0, brokerHistoryLimit),
	}
}

func (b *Broker) Subscribe(ctx context.Context) <-chan protocol.Event {
	return b.subscribeFrom(ctx, "")
}

func (b *Broker) SubscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event {
	return b.subscribeFrom(ctx, lastEventID)
}

func (b *Broker) Publish(event protocol.Event) {
	droppedCount := 0

	b.mu.Lock()
	b.history = append(b.history, event)
	if len(b.history) > brokerHistoryLimit {
		copy(b.history, b.history[len(b.history)-brokerHistoryLimit:])
		b.history = b.history[:brokerHistoryLimit]
	}
	// Non-blocking fan-out inside the single critical section: sends can never
	// block (buffered channels, drop-on-full), so holding b.mu here cannot
	// deadlock. Slow-subscriber logging is deferred until after unlock so no
	// arbitrary work runs under the lock.
	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
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
}

func (b *Broker) subscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	replay := b.eventsAfterLocked(lastEventID)

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
	b.subscribers[id] = ch
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()

		delete(b.subscribers, id)
		close(ch)
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
