package clisession

import (
	"context"
	"fmt"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// Notifier delivers a nudge into an already-open runtime session
// (runtime.mode: attached). It never starts the runtime.
type Notifier interface {
	Name() string
	Notify(ctx context.Context, config bridgeconfig.Config, text string) error
}

// NewAttachedRunner builds a runner that shares the managed runner's event
// stream, wake policy, and per-conversation queues, but nudges an open session
// through notifier instead of running a CLI command per wake.
func NewAttachedRunner(
	config bridgeconfig.Config,
	notifier Notifier,
	streamer eventStreamer,
	backoff backoffPolicy,
) *Runner {
	runner := NewRunner(config, nil, streamer, backoff)
	runner.notifier = notifier
	return runner
}

func RunAttached(
	ctx context.Context,
	config bridgeconfig.Config,
	notifier Notifier,
	streamer eventStreamer,
	backoff backoffPolicy,
) error {
	return NewAttachedRunner(config, notifier, streamer, backoff).Run(ctx)
}

func (r *Runner) name() string {
	if r.notifier != nil {
		return r.notifier.Name()
	}
	if r.turn != nil {
		return r.turn.Name()
	}
	return r.driver.Name()
}

// enqueueNudge coalesces the message into the conversation's pending nudge and
// starts a delivery worker if none is running.
func (r *Runner) enqueueNudge(ctx context.Context, event protocol.Event) error {
	if event.Message == nil {
		return fmt.Errorf("event has no message")
	}
	contextKey := SessionKey(r.config, event.Message)

	queue := r.queueFor(contextKey)
	queue.mu.Lock()
	defer queue.mu.Unlock()

	if queue.nudge == nil {
		queue.nudge = &Nudge{}
	}
	queue.nudge.Add(event.Message)
	if queue.running {
		r.emit(RunnerEventWakeQueued, RunnerEvent{
			ContextKey:  contextKey,
			MessageID:   event.Message.ID,
			QueuedCount: queue.nudge.Count,
		})
		return nil
	}

	queue.running = true
	r.queueWG.Add(1)
	go r.drainNudges(ctx, queue)
	return nil
}

func (r *Runner) drainNudges(ctx context.Context, queue *wakeQueue) {
	defer r.queueWG.Done()

	attempt := 0
	for {
		nudge := queue.takeNudge(ctx)
		if nudge == nil {
			r.removeQueueIfIdle(queue)
			return
		}

		err := r.notify(ctx, queue.contextKey, *nudge)
		if err == nil {
			attempt = 0
			continue
		}

		// Keep the undelivered nudge pending; anything that arrived meanwhile
		// coalesces into it instead of stacking a second nudge.
		queue.mu.Lock()
		nudge.Merge(queue.nudge)
		queue.nudge = nudge
		queue.mu.Unlock()

		attempt++
		observability.Logger(ctx, "bridge."+r.name(), "agent_id", r.config.Agent.ID, "context_key", queue.contextKey, "attempt", attempt, "error", err).
			Warn("attached session nudge failed; will retry")

		select {
		case <-ctx.Done():
		case <-time.After(r.backoff.Delay(attempt)):
		}
	}
}

// takeNudge removes the pending nudge, or marks the queue idle when there is
// none (or the runner is stopping).
func (q *wakeQueue) takeNudge(ctx context.Context) *Nudge {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ctx.Err() != nil || q.nudge == nil {
		q.running = false
		return nil
	}
	nudge := q.nudge
	q.nudge = nil
	return nudge
}

func (r *Runner) notify(ctx context.Context, contextKey string, nudge Nudge) error {
	payload := RunnerEvent{ContextKey: contextKey, QueuedCount: nudge.Count, RuntimeSessionID: r.config.Runtime.Session}
	if err := EnsureWorkspaceClientConfig(r.config); err != nil {
		payload.Error = err
		r.emit(RunnerEventTurnFailed, payload)
		return err
	}

	r.emit(RunnerEventTurnStarted, payload)
	if err := r.notifier.Notify(ctx, r.config, nudge.Text(r.config.Moltnet.NetworkID, r.config.Agent.ID)); err != nil {
		payload.Error = err
		r.emit(RunnerEventTurnFailed, payload)
		return err
	}
	observability.Logger(ctx, "bridge."+r.name(), "agent_id", r.config.Agent.ID, "context_key", contextKey, "messages", nudge.Count).
		Info("attached session nudged")
	r.emit(RunnerEventTurnCompleted, payload)
	return nil
}
