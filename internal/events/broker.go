package events

import (
	"context"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type Broker struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]chan protocol.Event
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[uint64]chan protocol.Event),
	}
}

func (b *Broker) Subscribe(ctx context.Context) <-chan protocol.Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := b.nextID
	ch := make(chan protocol.Event, 32)
	b.subscribers[id] = ch

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()

		delete(b.subscribers, id)
		close(ch)
	}()

	return ch
}

func (b *Broker) Publish(event protocol.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}
