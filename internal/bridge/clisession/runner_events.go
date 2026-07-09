package clisession

type RunnerEventType string

const (
	RunnerEventWakeQueued    RunnerEventType = "agent.wake.queued"
	RunnerEventTurnStarted   RunnerEventType = "agent.turn.started"
	RunnerEventTurnCompleted RunnerEventType = "agent.turn.completed"
	RunnerEventTurnFailed    RunnerEventType = "agent.turn.failed"
)

type RunnerEvent struct {
	ContextKey       string
	MessageID        string
	QueuedCount      int
	RuntimeSessionID string
	ExistingSession  bool
	Bootstrap        bool
	Error            error
}

type RunnerObserver func(eventType RunnerEventType, payload RunnerEvent)

func (r *Runner) SetObserver(observer RunnerObserver) {
	if observer == nil {
		r.observe = func(RunnerEventType, RunnerEvent) {}
		return
	}
	r.observe = observer
}

func (r *Runner) emit(eventType RunnerEventType, payload RunnerEvent) {
	if r == nil || r.observe == nil {
		return
	}
	r.observe(eventType, payload)
}

func (r *Runner) eventPayloadForDelivery(
	delivery Delivery,
	queueLength int,
	eventErr error,
) RunnerEvent {
	return RunnerEvent{
		ContextKey:       delivery.ContextKey,
		MessageID:        delivery.MessageID,
		QueuedCount:      queueLength,
		RuntimeSessionID: delivery.RuntimeSessionID,
		ExistingSession:  delivery.ExistingSession,
		Bootstrap:        delivery.Bootstrap,
		Error:            eventErr,
	}
}

func (r *Runner) failDelivery(delivery Delivery, err error) error {
	r.emit(RunnerEventTurnFailed, r.eventPayloadForDelivery(delivery, 0, err))
	return err
}
