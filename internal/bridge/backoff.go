package bridge

import (
	"math/rand/v2"
	"time"
)

const (
	DefaultReconnectBaseDelay = 500 * time.Millisecond
	DefaultReconnectMaxDelay  = 15 * time.Second
)

type Backoff struct {
	base time.Duration
	max  time.Duration
}

func NewBackoff(base time.Duration, max time.Duration) *Backoff {
	if base <= 0 {
		base = DefaultReconnectBaseDelay
	}
	if max < base {
		max = DefaultReconnectMaxDelay
	}

	return &Backoff{
		base: base,
		max:  max,
	}
}

func (b *Backoff) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := b.base
	for step := 1; step < attempt && delay < b.max; step++ {
		delay *= 2
		if delay > b.max {
			delay = b.max
		}
	}

	jitter := delay / 5
	if jitter <= 0 {
		return delay
	}

	spread := rand.Int64N(int64(jitter)*2 + 1)
	delay = delay - jitter + time.Duration(spread)
	if delay > b.max {
		return b.max
	}
	if delay < b.base {
		return b.base
	}

	return delay
}
