package loop

import (
	"context"
	"fmt"
	"net/http"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// newControlDeliveryBackoff is a var, not a plain call, solely so tests can
// swap in a near-zero backoff and exercise the retry-budget-exhausted
// branch without waiting out real reconnect-scale delays.
var newControlDeliveryBackoff = func() *bridgeutil.Backoff {
	return bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay)
}

// deliverControlMessage is the retry-storm fix's core: it wraps
// sendControlMessage in a bounded, per-event.ID retry with
// transient-vs-permanent classification, so a single failing control POST
// can no longer conflate "the runtime rejected this wake" with "the
// transport hiccuped" the way a bare `return err` out of the attachment
// handler used to.
//
// Legacy completion codecs return nil after a terminal delivery failure so a
// permanently failing wake is skipped instead of replay-storming forever.
// DurableAcceptanceCodec is different by contract: failed acceptance returns
// an error after bounded reporting, so StreamEventsReady does not ACK and the
// stored event remains replayable. Successful paths always ACK the cursor of
// the frame actually received, never a synthesized or skipped-ahead cursor.
func deliverControlMessage(
	ctx context.Context,
	controlClient *http.Client,
	client *MoltnetClient,
	config bridgeconfig.Config,
	codec ControlCodec,
	event protocol.Event,
	deliveries *controlDeliveryTracker,
) error {
	state := deliveries.stateFor(event.ID)
	requiresAcceptance := false
	if acceptanceCodec, ok := codec.(DurableAcceptanceCodec); ok {
		requiresAcceptance = acceptanceCodec.RequiresDurableAcceptance()
	}

	if state.permanent {
		// Idempotent re-delivery: this event.ID already exhausted its
		// retry budget or hit a permanent classification on a prior
		// connection (e.g. a resume-cursor gap replay resent it after
		// the bridge's cursor aged out of the server's replay
		// history). Report again for evidence, but never re-attempt
		// the control POST — that is exactly the storming behavior
		// this fix removes.
		reportWakeFailure(ctx, client, config, event, state.attempts, protocol.WakeFailureClassificationPermanent, state.lastErr)
		return nil
	}

	retryBackoff := newControlDeliveryBackoff()

	for {
		state.attempts++

		result, err := sendControlMessageWithCodec(ctx, controlClient, config, event, codec)
		if err == nil {
			if result.Acceptance != nil {
				asyncCodec, ok := codec.(AsyncControlCodec)
				if !ok {
					return fmt.Errorf("control codec returned asynchronous acceptance without a handler")
				}
				if err := asyncCodec.ControlAccepted(config, event, *result.Acceptance); err != nil {
					return fmt.Errorf("persist asynchronous control acceptance: %w", err)
				}
			}
			published, publishErr := publishControlResult(ctx, client, config, event.Message.Target, event.Message.ID, result)
			if publishErr == nil && published && result.PublishOnce {
				if observer, ok := codec.(ControlPublicationObserver); ok {
					observer.ControlResultPublished(ControlDelivery{EventID: protocol.MessageEventID(event.Message.ID)})
				}
			}
			return publishErr
		}

		switch classifyControlError(ctx, err) {
		case controlErrorFatal:
			return err
		case controlErrorPermanent:
			reportWakeFailure(ctx, client, config, event, state.attempts, protocol.WakeFailureClassificationPermanent, err)
			if requiresAcceptance {
				return err
			}
			state.permanent = true
			state.lastErr = err
			return nil
		default: // controlErrorTransient
			if state.attempts >= maxControlDeliveryAttempts {
				reportWakeFailure(ctx, client, config, event, state.attempts, protocol.WakeFailureClassificationRetryExhausted, err)
				if requiresAcceptance {
					return err
				}
				state.permanent = true
				state.lastErr = err
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryBackoff.Delay(state.attempts)):
			}
		}
	}
}

// reportWakeFailure tells the moltnet server about a terminal (permanent or
// retry-budget-exhausted) wake failure without tearing down the attachment
// socket, so the failure is ledger-visible instead of a silent skip. This
// is the bridge-side counterpart to publishControlResponse: same
// best-effort posture — a failing report here must never turn into a
// second error out of deliverControlMessage, since the caller has already
// committed to skipping (ACK + advance) this event.ID.
func reportWakeFailure(
	ctx context.Context,
	client *MoltnetClient,
	config bridgeconfig.Config,
	event protocol.Event,
	attempts int,
	classification string,
	causeErr error,
) {
	if event.Message == nil {
		return
	}

	message := ""
	if causeErr != nil {
		message = causeErr.Error()
	}

	// Best-effort: the report itself may fail (runtime unreachable,
	// server rejects it) without affecting the ACK/cursor-advance
	// decision the caller has already made.
	_ = client.ReportWakeFailed(ctx, protocol.ReportWakeFailedRequest{
		AgentID:        config.Agent.ID,
		Event:          event,
		Error:          message,
		Attempts:       attempts,
		Classification: classification,
	})
}
