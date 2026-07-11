package loop

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// maxControlDeliveryAttempts bounds how many times a single control-wake
// delivery (one event.ID) is retried before it is treated as permanently
// failed. This is what stops the retry-storm: a transient failure gets a
// handful of tries with backoff, but it can never retry forever the way an
// unclassified handler error used to (RunControlLoop reconnecting and
// replaying the same stale cursor on every attach).
//
// A var, not a const, solely so tests can lower it and exercise the
// retry-budget-exhausted path without waiting out real backoff delays.
var maxControlDeliveryAttempts = 5

// maxTrackedControlDeliveries bounds the controlDeliveryTracker's memory: a
// long-lived bridge process must not accumulate one entry per event.ID
// forever. Oldest entries are evicted first; losing a stale entry only
// means a very old, already-resolved event.ID would restart its retry
// budget from zero if somehow redelivered, which cannot happen once the
// server's bounded replay history (256 events) has moved past it.
const maxTrackedControlDeliveries = 512

// controlErrorClass characterizes a control-delivery failure so the retry
// loop (deliverControlMessage in control.go) knows whether another attempt
// is worth making.
type controlErrorClass int

const (
	// controlErrorFatal means the surrounding context ended (bridge
	// shutdown, or an already-cancelled RunControlLoop context). Do not
	// retry and do not treat this as a wake failure: return the error
	// as-is so the caller unwinds the attachment exactly like it did
	// before this fix, matching
	// TestMoltnetClientPostReadyErrorAndHandlerError/handler_error's
	// "genuinely fatal" contract.
	controlErrorFatal controlErrorClass = iota
	// controlErrorTransient means the failure looks like a temporary
	// delivery problem (network hiccup, timeout, 5xx, 429) worth
	// retrying under budget.
	controlErrorTransient
	// controlErrorPermanent means retrying is very unlikely to help
	// (4xx other than 429, a malformed control response, or a local
	// request-shape error) — skip straight to a terminal wake failure.
	controlErrorPermanent
)

// classifyControlError decides whether a sendControlMessage failure is a
// context shutdown, a transient delivery problem worth retrying, or a
// permanent one that should be reported and skipped (ACK + cursor advance)
// rather than replayed forever.
//
// This mirrors the narrow string-classification precedent in
// internal/bridge/clisession/retry.go's shouldRetryWithFreshSession: no new
// typed-error taxonomy on sendControlText, just recognizable substrings on
// the already-wrapped error message.
func classifyControlError(ctx context.Context, err error) controlErrorClass {
	if err == nil {
		return controlErrorTransient
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return controlErrorFatal
	}

	message := strings.ToLower(err.Error())

	switch {
	case strings.Contains(message, "control url returned"):
		return classifyControlStatusMessage(message)
	case strings.Contains(message, "decode control response"),
		strings.Contains(message, "encode control request"),
		strings.Contains(message, "build control request"),
		strings.Contains(message, "control wake requires"),
		strings.Contains(message, "event has no message"):
		// Malformed response shape or a local request-construction /
		// event-shape problem: retrying the same event will not
		// change the outcome.
		return controlErrorPermanent
	default:
		// Network-level failures ("request control url ...": connection
		// refused, timeout, DNS, EOF) — treat as transient so a flaky
		// runtime endpoint gets a bounded number of retries before the
		// event is skipped.
		return controlErrorTransient
	}
}

// classifyControlStatusMessage classifies a "control url returned <status>"
// message by its HTTP status code: 5xx and 429 are treated as transient
// (server overload / rate limit, worth another attempt); every other 4xx is
// treated as permanent (the request itself is unacceptable to the runtime,
// retrying it verbatim will not help).
func classifyControlStatusMessage(message string) controlErrorClass {
	const marker = "returned "
	idx := strings.Index(message, marker)
	if idx == -1 {
		return controlErrorPermanent
	}
	code := message[idx+len(marker):]
	switch {
	case strings.HasPrefix(code, "429"):
		return controlErrorTransient
	case strings.HasPrefix(code, "5"):
		return controlErrorTransient
	default:
		return controlErrorPermanent
	}
}

// controlDeliveryState tracks retry progress for one event.ID's control
// delivery. Once permanent is set, later deliveries of the same event.ID
// (e.g. a resume-cursor gap replay resending history after the bridge's
// cursor aged out of the server's retention window) skip the retry budget
// entirely instead of re-storming the control endpoint: the outcome is
// already known.
type controlDeliveryState struct {
	attempts  int
	permanent bool
	lastErr   error
}

// controlDeliveryTracker is created once per RunControlLoop call (outside
// its reconnect loop) and threaded into every connection's handle closure,
// so a permanently-failed event.ID stays known across reconnects — the
// structural fix for "every reconnect restarts retry at 0". It is not
// persisted across process restarts; that is acceptable because a fresh
// process re-attaches with whatever cursor it last durably advanced past,
// and a genuinely permanent failure will simply be classified permanent
// again on first (re-)encounter, in bounded time.
type controlDeliveryTracker struct {
	mu    sync.Mutex
	order []string
	state map[string]*controlDeliveryState
}

func newControlDeliveryTracker() *controlDeliveryTracker {
	return &controlDeliveryTracker{
		state: make(map[string]*controlDeliveryState),
	}
}

// stateFor returns the mutable retry state for eventID, creating it on
// first use and evicting the oldest tracked entry once the tracker is at
// capacity.
func (t *controlDeliveryTracker) stateFor(eventID string) *controlDeliveryState {
	t.mu.Lock()
	defer t.mu.Unlock()

	if state, ok := t.state[eventID]; ok {
		return state
	}

	state := &controlDeliveryState{}
	t.state[eventID] = state
	t.order = append(t.order, eventID)
	if len(t.order) > maxTrackedControlDeliveries {
		oldest := t.order[0]
		t.order = t.order[1:]
		delete(t.state, oldest)
	}
	return state
}
