package bridge

import (
	"testing"
	"time"
)

func TestBackoffDelayRange(t *testing.T) {
	t.Parallel()

	backoff := NewBackoff(500*time.Millisecond, 4*time.Second)
	first := backoff.Delay(1)
	if first < 400*time.Millisecond || first > 600*time.Millisecond {
		t.Fatalf("unexpected first delay %v", first)
	}

	later := backoff.Delay(4)
	if later < 3200*time.Millisecond || later > 4*time.Second {
		t.Fatalf("unexpected capped delay %v", later)
	}

	defaulted := NewBackoff(0, 0).Delay(1)
	if defaulted <= 0 {
		t.Fatalf("expected positive default delay, got %v", defaulted)
	}
}

func TestBackoffDelayHandlesTinyBase(t *testing.T) {
	t.Parallel()

	backoff := NewBackoff(time.Nanosecond, 10*time.Nanosecond)
	if delay := backoff.Delay(1); delay != time.Nanosecond {
		t.Fatalf("unexpected tiny-base delay %v", delay)
	}
}
