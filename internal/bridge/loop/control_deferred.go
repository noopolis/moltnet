package loop

import (
	"errors"
	"time"
)

const (
	minControlDeferredDelay = time.Second
	maxControlDeferredDelay = 5 * time.Minute
)

// ControlDeferredError means the runtime has not accepted ownership yet, but
// may accept the same delivery later. It is backpressure, not a failed wake:
// leave the attachment cursor unchanged, then retry after a bounded delay.
// Runtime codecs validate the wire reason before constructing this error.
type ControlDeferredError struct {
	Reason     string
	RetryAfter time.Duration
}

func (e *ControlDeferredError) Error() string { return "runtime work deferred: " + e.Reason }

func controlDeferral(err error) (*ControlDeferredError, bool) {
	var deferred *ControlDeferredError
	ok := errors.As(err, &deferred)
	return deferred, ok
}

func controlReconnectDelay(err error, fallback time.Duration) time.Duration {
	deferred, ok := controlDeferral(err)
	if !ok {
		return fallback
	}
	delay := deferred.RetryAfter
	if delay < minControlDeferredDelay {
		return minControlDeferredDelay
	}
	if delay > maxControlDeferredDelay {
		return maxControlDeferredDelay
	}
	return delay
}
