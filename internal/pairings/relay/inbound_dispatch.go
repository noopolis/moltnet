package relay

import (
	"context"
	"sync"
)

// inboundDispatcher bounds active inbound handlers for one Run invocation.
// stopAndWait prevents future admissions before waiting, so WaitGroup.Add never
// races with shutdown's Wait.
type inboundDispatcher struct {
	sem chan struct{}
	wg  sync.WaitGroup

	mu      sync.Mutex
	stopped bool
}

func newInboundDispatcher(limit int) *inboundDispatcher {
	return &inboundDispatcher{sem: make(chan struct{}, limit)}
}

func (d *inboundDispatcher) start(ctx context.Context, serve func()) bool {
	select {
	case d.sem <- struct{}{}:
	case <-ctx.Done():
		return false
	}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		<-d.sem
		return false
	}
	d.wg.Add(1)
	d.mu.Unlock()

	go func() {
		defer func() {
			<-d.sem
			d.wg.Done()
		}()
		serve()
	}()
	return true
}

func (d *inboundDispatcher) stopAndWait() {
	d.mu.Lock()
	d.stopped = true
	d.mu.Unlock()
	d.wg.Wait()
}
