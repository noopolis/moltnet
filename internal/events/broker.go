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

type Broker struct {
	mu          sync.Mutex
	nextID      uint64
	subsMu      sync.RWMutex
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
	b.mu.Lock()
	b.history = append(b.history, event)
	if len(b.history) > brokerHistoryLimit {
		copy(b.history, b.history[len(b.history)-brokerHistoryLimit:])
		b.history = b.history[:brokerHistoryLimit]
	}
	b.mu.Unlock()

	b.subsMu.RLock()
	defer b.subsMu.RUnlock()
	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
			observability.DefaultMetrics.RecordDroppedEvent()
			observability.Logger(context.Background(), "events.broker", "event_id", event.ID).
				Warn("drop event for slow subscriber")
		}
	}
}

func (b *Broker) subscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	replay := b.eventsAfterLocked(lastEventID)
	b.mu.Unlock()

	bufferSize := brokerHistoryLimit
	if len(replay)+16 > bufferSize {
		bufferSize = len(replay) + 16
	}
	ch := make(chan protocol.Event, bufferSize)
	for _, event := range replay {
		ch <- event
	}
	b.subsMu.Lock()
	b.subscribers[id] = ch
	b.subsMu.Unlock()

	go func() {
		<-ctx.Done()
		b.subsMu.Lock()
		defer b.subsMu.Unlock()

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
