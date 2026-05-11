package clisession

import (
	"context"
	"fmt"
	"strings"
	"sync"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const maxQueuedWakeDeliveries = 256

type queuedDelivery struct {
	delivery Delivery
	message  *protocol.Message
}

type wakeQueue struct {
	contextKey string
	mu         sync.Mutex
	pending    []queuedDelivery
	running    bool
}

func (r *Runner) enqueueEventDelivery(ctx context.Context, event protocol.Event) error {
	delivery, err := r.eventDelivery(event)
	if err != nil {
		return err
	}

	contextKey := strings.TrimSpace(delivery.ContextKey)
	if contextKey == "" {
		contextKey = "main"
	}
	delivery.ContextKey = contextKey

	queue := r.queueFor(contextKey)
	queue.mu.Lock()
	defer queue.mu.Unlock()

	if len(queue.pending) >= maxQueuedWakeDeliveries {
		return fmt.Errorf("CLI runtime wake queue %q is full", contextKey)
	}

	queue.pending = append(queue.pending, queuedDelivery{
		delivery: delivery,
		message:  cloneMessage(event.Message),
	})
	if queue.running {
		return nil
	}

	queue.running = true
	r.queueWG.Add(1)
	go r.drainQueue(ctx, queue)
	return nil
}

func (r *Runner) queueFor(contextKey string) *wakeQueue {
	r.mu.Lock()
	defer r.mu.Unlock()

	queue, ok := r.queues[contextKey]
	if !ok {
		queue = &wakeQueue{contextKey: contextKey}
		r.queues[contextKey] = queue
	}
	return queue
}

func (r *Runner) drainQueue(ctx context.Context, queue *wakeQueue) {
	defer r.queueWG.Done()

	for {
		if ctx.Err() != nil {
			queue.mu.Lock()
			queue.running = false
			queue.mu.Unlock()
			return
		}

		batch := queue.nextBatch()
		if len(batch) == 0 {
			r.removeQueueIfIdle(queue)
			return
		}

		delivery := queuedBatchDelivery(r.config.Moltnet.NetworkID, batch)
		if err := r.dispatch(ctx, delivery); err != nil {
			observability.Logger(ctx, "bridge."+r.driver.Name(), "agent_id", r.config.Agent.ID, "error", err, "context_key", queue.contextKey).
				Warn("CLI runtime queued delivery failed")
		}
	}
}

func (q *wakeQueue) nextBatch() []queuedDelivery {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		q.running = false
		return nil
	}

	batch := append([]queuedDelivery(nil), q.pending...)
	q.pending = nil
	return batch
}

func (r *Runner) removeQueueIfIdle(queue *wakeQueue) {
	r.mu.Lock()
	defer r.mu.Unlock()

	queue.mu.Lock()
	defer queue.mu.Unlock()

	if queue.running || len(queue.pending) > 0 {
		return
	}
	if r.queues[queue.contextKey] == queue {
		delete(r.queues, queue.contextKey)
	}
}

func (r *Runner) waitForQueues() {
	r.queueWG.Wait()
}

func queuedBatchDelivery(networkID string, batch []queuedDelivery) Delivery {
	if len(batch) == 0 {
		return Delivery{}
	}
	if len(batch) == 1 {
		return batch[0].delivery
	}

	first := batch[0].delivery
	messages := make([]*protocol.Message, 0, len(batch))
	messageIDs := make([]string, 0, len(batch))
	for _, item := range batch {
		if item.message != nil {
			messages = append(messages, item.message)
		}
		if trimmed := strings.TrimSpace(item.delivery.MessageID); trimmed != "" {
			messageIDs = append(messageIDs, trimmed)
		}
	}

	first.Prompt = renderQueuedInboundMessages(networkID, messages)
	if len(messageIDs) > 0 {
		first.MessageID = messageIDs[len(messageIDs)-1]
	}
	return first
}

func renderQueuedInboundMessages(networkID string, messages []*protocol.Message) string {
	if len(messages) == 0 {
		return ""
	}

	lines := []string{
		"Channel: moltnet",
		"Chat ID: " + bridgeutil.ChatID(networkID, messages[0].Target, messages[0].ID),
		fmt.Sprintf("Queued messages: %d", len(messages)),
		"These messages arrived while this runtime was already active. Review them in order and respond once if appropriate.",
	}

	for index, message := range messages {
		lines = append(lines,
			"",
			fmt.Sprintf("--- Message %d/%d ---", index+1, len(messages)),
			bridgeutil.RenderCompactInboundMessage(networkID, message, false),
		)
	}

	return strings.Join(lines, "\n")
}

func cloneMessage(message *protocol.Message) *protocol.Message {
	if message == nil {
		return nil
	}
	clone := *message
	clone.Mentions = append([]string(nil), message.Mentions...)
	clone.Parts = append([]protocol.Part(nil), message.Parts...)
	clone.Target.ParticipantIDs = append([]string(nil), message.Target.ParticipantIDs...)
	return &clone
}
