package loop

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type deferredTestCodec struct{ *legacyControlCodec }

func (*deferredTestCodec) DecodeResponse(bridgeconfig.Config, ControlDelivery, *http.Response) (ControlResult, error) {
	return ControlResult{}, &ControlDeferredError{Reason: "operator_stop", RetryAfter: time.Minute}
}

func TestDeferredDeliveryDoesNotConsumeFailureBudgetOrReportFailure(t *testing.T) {
	harness := newControlRetryTestHarness(t, http.StatusConflict, func(*testing.T, *websocket.Conn, int) {})
	config := harness.config()
	deliveries := newControlDeliveryTracker()
	for attempt := 0; attempt < maxControlDeliveryAttempts+2; attempt++ {
		err := deliverControlMessage(t.Context(), http.DefaultClient, NewMoltnetClient(config), config,
			&deferredTestCodec{legacyControlCodec: &legacyControlCodec{}}, *permanentlyFailingEvent("evt_deferred"), deliveries)
		if _, ok := controlDeferral(err); !ok {
			t.Fatalf("expected non-ACKing deferral, got %v", err)
		}
	}
	_, requests, reports := harness.counts()
	state := deliveries.stateFor("evt_deferred")
	if requests != maxControlDeliveryAttempts+2 || reports != 0 || state.attempts != 0 || state.permanent {
		t.Fatalf("deferral lost retryability or reported failure: requests=%d reports=%d state=%#v", requests, reports, state)
	}
}

func TestControlDeferralReconnectDelayIsBounded(t *testing.T) {
	for _, test := range []struct{ input, want time.Duration }{
		{0, time.Second}, {-time.Second, time.Second}, {time.Millisecond, time.Second},
		{30 * time.Second, 30 * time.Second}, {time.Hour, 5 * time.Minute},
	} {
		err := fmt.Errorf("wrapped: %w", &ControlDeferredError{Reason: "operator_stop", RetryAfter: test.input})
		if got := controlReconnectDelay(err, time.Millisecond); got != test.want {
			t.Fatalf("delay(%s)=%s, want %s", test.input, got, test.want)
		}
	}
	if got := controlReconnectDelay(fmt.Errorf("legacy error"), time.Millisecond); got != time.Millisecond {
		t.Fatalf("changed legacy delay: %s", got)
	}
}

func TestControlDeferralDoesNotSendAttachmentError(t *testing.T) {
	err := &ControlDeferredError{Reason: "operator_stop", RetryAfter: time.Minute}
	got := reportAttachmentHandlerError(func(protocol.AttachmentFrame) error {
		t.Fatal("backpressure published an attachment error")
		return nil
	}, err)
	if got != err {
		t.Fatalf("lost replay signal: %v", got)
	}
}

func TestDeferredReconnectWaitCanBeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	harness := newControlRetryTestHarness(t, http.StatusConflict, func(t *testing.T, conn *websocket.Conn, attempt int) {
		if attempt > 1 {
			t.Error("retried before the deferred delay expired")
		}
		attachHandshake(t, conn)
		event := permanentlyFailingEvent("evt_deferred")
		_ = conn.WriteJSON(protocol.AttachmentFrame{Op: protocol.AttachmentOpEvent, Version: protocol.AttachmentProtocolV1, Cursor: event.ID, Event: event})
		var frame protocol.AttachmentFrame
		if err := conn.ReadJSON(&frame); err == nil {
			t.Errorf("deferred delivery emitted frame %#v", frame)
		}
		cancel()
	})
	started := time.Now()
	if err := RunControlLoopWithCodec(ctx, harness.config(), &deferredTestCodec{legacyControlCodec: &legacyControlCodec{}}); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("shutdown waited out the deferred retry delay")
	}
	_, requests, reports := harness.counts()
	if requests != 1 || reports != 0 {
		t.Fatalf("requests=%d reports=%d, want 1/0", requests, reports)
	}
}
