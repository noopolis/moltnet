package clisession

import (
	"context"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

// TurnExecutor runs a managed turn over a runtime-specific session transport.
// It returns a session ID to persist when the runtime supports resuming it.
type TurnExecutor interface {
	Name() string
	Turn(ctx context.Context, delivery Delivery) (sessionID string, err error)
}

func NewTurnRunner(
	config bridgeconfig.Config,
	executor TurnExecutor,
	streamer eventStreamer,
	backoff backoffPolicy,
) *Runner {
	runner := NewRunner(config, nil, streamer, backoff)
	runner.turn = executor
	return runner
}

func (r *Runner) dispatchTurn(ctx context.Context, delivery Delivery) error {
	r.emit(RunnerEventTurnStarted, r.eventPayloadForDelivery(delivery, 0, nil))
	sessionID, err := r.turn.Turn(ctx, delivery)
	if err != nil {
		return r.failDelivery(delivery, err)
	}

	if sessionID != "" && sessionID != delivery.RuntimeSessionID {
		if _, err := r.store.Put(delivery.ContextKey, sessionID); err != nil {
			return r.failDelivery(delivery, err)
		}
		delivery.RuntimeSessionID = sessionID
	}
	r.emit(RunnerEventTurnCompleted, r.eventPayloadForDelivery(delivery, 0, nil))
	return nil
}
