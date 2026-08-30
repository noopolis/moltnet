package daimon

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

var (
	receiptPollBaseDelay = 500 * time.Millisecond
	receiptPollMaxDelay  = 30 * time.Second
)

type receiptTracker struct {
	store       *receiptStore
	token       protocol.SecretString
	moltnet     *loop.MoltnetClient
	config      bridgeconfig.Config
	httpClient  *http.Client
	notify      chan struct{}
	pollBackoff *bridgeutil.Backoff
}

type receiptPollState struct {
	attempts int
	next     time.Time
}

func newReceiptTracker(store *receiptStore, token protocol.SecretString, moltnet *loop.MoltnetClient, config bridgeconfig.Config) *receiptTracker {
	return &receiptTracker{
		store:       store,
		token:       token,
		moltnet:     moltnet,
		config:      config,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		notify:      make(chan struct{}, 1),
		pollBackoff: bridgeutil.NewBackoff(receiptPollBaseDelay, receiptPollMaxDelay),
	}
}

func (t *receiptTracker) Accept(job receiptJob) error {
	if err := t.store.Put(job); err != nil {
		return err
	}
	select {
	case t.notify <- struct{}{}:
	default:
	}
	return nil
}

func (t *receiptTracker) Run(ctx context.Context) {
	schedule := make(map[string]receiptPollState)
	for {
		jobs := t.store.Pending()
		if len(jobs) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-t.notify:
				continue
			}
		}

		now := time.Now()
		nextWake := receiptPollMaxDelay
		for _, job := range jobs {
			state := schedule[job.AcceptanceID]
			if state.next.After(now) {
				if delay := time.Until(state.next); delay < nextWake {
					nextWake = delay
				}
				continue
			}
			if t.follow(ctx, job) {
				delete(schedule, job.AcceptanceID)
				continue
			}
			if ctx.Err() != nil {
				return
			}
			state.attempts++
			state.next = time.Now().Add(t.pollBackoff.Delay(state.attempts))
			schedule[job.AcceptanceID] = state
			if delay := time.Until(state.next); delay < nextWake {
				nextWake = delay
			}
		}
		if nextWake < 0 {
			nextWake = 0
		}
		timer := time.NewTimer(nextWake)
		select {
		case <-ctx.Done():
			stopReceiptTimer(timer)
			return
		case <-t.notify:
			stopReceiptTimer(timer)
		case <-timer.C:
		}
	}
}

func stopReceiptTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func (t *receiptTracker) follow(ctx context.Context, job receiptJob) bool {
	receipt, err := t.fetch(ctx, job)
	if err != nil {
		return false
	}
	switch receipt.State {
	case "accepted", "running":
		return false
	case "completed":
		if !receipt.HasText || strings.TrimSpace(receipt.Text) == "" {
			return t.store.MarkTerminal(job.AcceptanceID, receiptJobNoReply, "", time.Now()) == nil
		}
		return t.publish(ctx, job, receipt.Text)
	case "failed", "stopped":
		return t.reportTerminalFailure(ctx, job, receipt)
	default:
		return false
	}
}

func (t *receiptTracker) fetch(ctx context.Context, job receiptJob) (wakeReceipt, error) {
	endpoint := strings.TrimRight(t.config.Runtime.ControlURL, "/") + "/v2/wake-receipts/" + job.AcceptanceID
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return wakeReceipt{}, fmt.Errorf("build Daimon wake receipt request: %w", err)
	}
	if token := strings.TrimSpace(t.token.Reveal()); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := t.httpClient.Do(request)
	if err != nil {
		return wakeReceipt{}, fmt.Errorf("request Daimon wake receipt: %w", err)
	}
	defer response.Body.Close()
	return decodeWakeReceipt(response, loop.ControlAcceptance{
		ID:            job.AcceptanceID,
		AgentID:       job.RuntimeAgentID,
		DeliveryID:    job.DeliveryID,
		RequestDigest: job.RequestDigest,
		AcceptedAt:    job.AcceptedAt,
	})
}

func (t *receiptTracker) publish(ctx context.Context, job receiptJob, text string) bool {
	messageID := bridgeutil.DeliveryReplyMessageID(job.MoltnetAgent.ID, job.DeliveryID, job.Event.Message.Target)
	accepted, err := t.moltnet.SendMessage(ctx, protocol.SendMessageRequest{
		ID:     messageID,
		Target: job.Event.Message.Target,
		From:   job.MoltnetAgent,
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: strings.TrimSpace(text)}},
		CauseEventIDs: []string{
			"daimon:" + job.DeliveryID + ":turn.output.completed",
		},
	})
	if err != nil || !accepted.Accepted || accepted.MessageID != messageID {
		return false
	}
	return t.store.MarkTerminal(job.AcceptanceID, receiptJobPublished, "", time.Now()) == nil
}

func (t *receiptTracker) reportTerminalFailure(ctx context.Context, job receiptJob, receipt wakeReceipt) bool {
	classification := protocol.WakeFailureClassificationRuntimeFailed
	terminalState := receiptJobRuntimeFailed
	if receipt.State == "stopped" {
		classification = protocol.WakeFailureClassificationRuntimeStopped
		terminalState = receiptJobRuntimeStopped
	}
	message := "Daimon wake " + receipt.State
	if receipt.Code != "" {
		message += ": " + receipt.Code
	}
	if err := t.moltnet.ReportWakeFailed(ctx, protocol.ReportWakeFailedRequest{
		AgentID:        job.MoltnetAgent.ID,
		Event:          job.Event,
		Error:          message,
		Attempts:       1,
		Classification: classification,
	}); err != nil {
		return false
	}
	return t.store.MarkTerminal(job.AcceptanceID, terminalState, receipt.Code, time.Now()) == nil
}
