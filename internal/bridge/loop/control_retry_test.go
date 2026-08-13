package loop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// controlRetryTestHarness is the deterministic storm-repro rig for the
// retry-storm fix: a fake /v1/attach + /v1/agents/wake-failed moltnet
// server, and a fake control server whose failure mode is configurable per
// test. It exists so each storm-repro test only has to describe *what*
// the control server does, not the attach/identify/ready handshake noise.
type controlRetryTestHarness struct {
	t *testing.T

	mu               sync.Mutex
	attachRequests   int
	controlRequests  int
	wakeFailedBodies []protocol.ReportWakeFailedRequest

	controlStatus int // HTTP status the control server always returns.

	moltnetServer *httptest.Server
	controlServer *httptest.Server
}

// newControlRetryTestHarness starts both fake servers. onAttach is invoked
// once per /v1/attach upgrade (test supplies the event-sending / ack-reading
// script for that connection).
func newControlRetryTestHarness(
	t *testing.T,
	controlStatus int,
	onAttach func(t *testing.T, connection *websocket.Conn, attachAttempt int),
) *controlRetryTestHarness {
	t.Helper()

	harness := &controlRetryTestHarness{t: t, controlStatus: controlStatus}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	harness.moltnetServer = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/attach":
			harness.mu.Lock()
			harness.attachRequests++
			attempt := harness.attachRequests
			harness.mu.Unlock()

			connection, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				t.Fatalf("upgrade websocket: %v", err)
			}
			defer connection.Close()
			onAttach(t, connection, attempt)
		case "/v1/agents/wake-failed":
			defer request.Body.Close()
			var payload protocol.ReportWakeFailedRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode wake failed report: %v", err)
			}
			harness.mu.Lock()
			harness.wakeFailedBodies = append(harness.wakeFailedBodies, payload)
			harness.mu.Unlock()
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusAccepted)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))

	harness.controlServer = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() { _, _ = io.Copy(io.Discard, request.Body) }()
		harness.mu.Lock()
		harness.controlRequests++
		harness.mu.Unlock()
		response.WriteHeader(harness.controlStatus)
	}))

	t.Cleanup(func() {
		harness.moltnetServer.Close()
		harness.controlServer.Close()
	})

	return harness
}

func (h *controlRetryTestHarness) config() bridgeconfig.Config {
	return bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: h.moltnetServer.URL, NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: h.controlServer.URL},
		// WakeMentions, not WakeAll: WakeAll (bridgeutil.ShouldBootstrap)
		// triggers a bootstrap control POST on READY, which would also
		// hit — and fail against — the always-failing control server
		// below and confound these tests with an unrelated bootstrap
		// failure. TestRunControlLoopReconnectsAfterAttachFailure uses
		// the same WakeMentions choice for the same reason.
		Rooms: []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeMentions}},
	}
}

func (h *controlRetryTestHarness) counts() (attachRequests, controlRequests, wakeFailedReports int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attachRequests, h.controlRequests, len(h.wakeFailedBodies)
}

func (h *controlRetryTestHarness) lastWakeFailedReport() protocol.ReportWakeFailedRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.wakeFailedBodies) == 0 {
		h.t.Fatal("expected at least one wake failed report")
	}
	return h.wakeFailedBodies[len(h.wakeFailedBodies)-1]
}

// permanentlyFailingEvent is the single message.created event every
// storm-repro test in this file pushes down the attachment: a targeted
// room wake that the fake control server will refuse forever.
func permanentlyFailingEvent(cursor string) *protocol.Event {
	return &protocol.Event{
		ID:        cursor,
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_" + cursor,
			NetworkID: "local",
			Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
			From:      protocol.Actor{Type: "agent", ID: "writer"},
			Mentions:  []string{"researcher"},
			Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
			CreatedAt: time.Now().UTC(),
		},
		CreatedAt: time.Now().UTC(),
	}
}

func attachHandshake(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:                  protocol.AttachmentOpHello,
		Version:             protocol.AttachmentProtocolV1,
		HeartbeatIntervalMS: 30000,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var identify protocol.AttachmentFrame
	if err := connection.ReadJSON(&identify); err != nil {
		t.Fatalf("read identify: %v", err)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpReady,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		AgentID:   "researcher",
	}); err != nil {
		t.Fatalf("write ready: %v", err)
	}
}

func sendEventAndExpectAck(t *testing.T, connection *websocket.Conn, event *protocol.Event) {
	t.Helper()
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpEvent,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Cursor:    event.ID,
		Event:     event,
	}); err != nil {
		t.Fatalf("write event: %v", err)
	}

	var ack protocol.AttachmentFrame
	if err := connection.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if ack.Op != protocol.AttachmentOpAck || ack.Cursor != event.ID {
		t.Fatalf(
			"expected ack for the received event's own cursor %q (never a synthesized/skipped-ahead cursor), got %#v",
			event.ID, ack,
		)
	}
}

// TestRunControlLoopSkipsPermanentlyFailingWake is the deterministic
// storm-repro test for the retry-storm bug: a control server that always
// answers 400 (an unclassified error used to be conflated with a transport
// failure, unwinding the stream with no ACK and no cursor advance, so
// RunControlLoop reconnected and identify() replayed the exact same event
// forever — 29x observed in the field).
//
// Pre-fix, this test would hang: the fake attach handler always waits for
// an ACK of the event it just sent before closing, and a bare
// `return err` out of the handler here means that ACK never comes on any
// connection, so RunControlLoop keeps reconnecting into
// harness.moltnetServer's /v1/attach handler without limit and the test
// times out.
//
// Post-fix, classifyControlError recognizes "control url returned 400" as
// permanent, deliverControlMessage reports exactly one agent.wake.failed
// and returns nil, and moltnet.go's existing success path (unchanged) ACKs
// result.frame.Cursor and advances past it — in a single connection, no
// reconnect required at all.
func TestRunControlLoopSkipsPermanentlyFailingWake(t *testing.T) {
	t.Parallel()

	harness := newControlRetryTestHarness(t, http.StatusBadRequest, func(t *testing.T, connection *websocket.Conn, attempt int) {
		if attempt > 1 {
			t.Fatalf("expected a single attach connection, got a reconnect (attempt %d) — the retry storm is back", attempt)
		}
		attachHandshake(t, connection)
		sendEventAndExpectAck(t, connection, permanentlyFailingEvent("evt_1"))
		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
			time.Now().Add(time.Second),
		)
	})

	if err := RunControlLoop(context.Background(), harness.config()); err != nil {
		t.Fatalf("RunControlLoop() error = %v", err)
	}

	attachRequests, controlRequests, wakeFailedReports := harness.counts()
	if attachRequests != 1 {
		t.Fatalf("expected exactly one attach connection (no reconnect storm), got %d", attachRequests)
	}
	if controlRequests != 1 {
		t.Fatalf("expected exactly one control POST for a permanent failure (no retry), got %d", controlRequests)
	}
	if wakeFailedReports != 1 {
		t.Fatalf("expected exactly one agent.wake.failed-shaped signal, got %d", wakeFailedReports)
	}

	report := harness.lastWakeFailedReport()
	if report.AgentID != "researcher" ||
		report.Event.ID != "evt_1" ||
		report.Classification != protocol.WakeFailureClassificationPermanent ||
		report.Attempts != 1 {
		t.Fatalf("unexpected wake failed report %#v", report)
	}
}

// TestRunControlLoopSkipsExhaustedTransientFailingWake covers the other
// classification branch: a control server that looks transient (500) on
// every attempt. deliverControlMessage retries under budget before
// reporting agent.wake.failed as retry_exhausted and skipping — it must
// still converge, not retry forever, once the budget above
// maxControlDeliveryAttempts is spent.
func TestRunControlLoopSkipsExhaustedTransientFailingWake(t *testing.T) {
	originalBudget := maxControlDeliveryAttempts
	originalBackoff := newControlDeliveryBackoff
	maxControlDeliveryAttempts = 3
	newControlDeliveryBackoff = func() *bridgeutil.Backoff {
		return bridgeutil.NewBackoff(time.Millisecond, time.Millisecond)
	}
	t.Cleanup(func() {
		maxControlDeliveryAttempts = originalBudget
		newControlDeliveryBackoff = originalBackoff
	})

	harness := newControlRetryTestHarness(t, http.StatusServiceUnavailable, func(t *testing.T, connection *websocket.Conn, attempt int) {
		if attempt > 1 {
			t.Fatalf("expected a single attach connection, got a reconnect (attempt %d) — the retry storm is back", attempt)
		}
		attachHandshake(t, connection)
		sendEventAndExpectAck(t, connection, permanentlyFailingEvent("evt_1"))
		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
			time.Now().Add(time.Second),
		)
	})

	if err := RunControlLoop(context.Background(), harness.config()); err != nil {
		t.Fatalf("RunControlLoop() error = %v", err)
	}

	attachRequests, controlRequests, wakeFailedReports := harness.counts()
	if attachRequests != 1 {
		t.Fatalf("expected exactly one attach connection (no reconnect storm), got %d", attachRequests)
	}
	if controlRequests != 3 {
		t.Fatalf("expected exactly maxControlDeliveryAttempts (3) control POSTs, got %d", controlRequests)
	}
	if wakeFailedReports != 1 {
		t.Fatalf("expected exactly one agent.wake.failed-shaped signal, got %d", wakeFailedReports)
	}

	report := harness.lastWakeFailedReport()
	if report.Classification != protocol.WakeFailureClassificationRetryExhausted || report.Attempts != 3 {
		t.Fatalf("unexpected wake failed report %#v", report)
	}
}

// TestRunControlLoopRedeliveredPermanentWakeSkipsRetryStorm covers the
// idempotent-to-redelivery coupling with the wake-race fix: if the bridge's
// cursor ages out of the server's replay history, the server resends an
// evt_gap_ plus full history and the same already-failed wake can arrive
// again. The bridge must not re-storm the control endpoint for a wake it
// already knows is permanently failing — it should recognize the
// redelivered event.ID and skip straight to another cheap, idempotent
// terminal report.
func TestRunControlLoopRedeliveredPermanentWakeSkipsRetryStorm(t *testing.T) {
	t.Parallel()

	harness := newControlRetryTestHarness(t, http.StatusBadRequest, func(t *testing.T, connection *websocket.Conn, attempt int) {
		if attempt > 2 {
			t.Fatalf("expected at most two attach connections, got a third (attempt %d) — the retry storm is back", attempt)
		}
		attachHandshake(t, connection)
		// Both connections replay the exact same event.ID, as a
		// resume-cursor gap replay would after the bridge's cursor
		// aged out of the server's retention window.
		sendEventAndExpectAck(t, connection, permanentlyFailingEvent("evt_1"))

		if attempt == 1 {
			// Simulate the connection dying non-gracefully so
			// RunControlLoop reconnects (unlike the other two tests,
			// this one deliberately forces a second /v1/attach to
			// exercise cross-reconnect idempotency).
			return
		}
		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
			time.Now().Add(time.Second),
		)
	})

	if err := RunControlLoop(context.Background(), harness.config()); err != nil {
		t.Fatalf("RunControlLoop() error = %v", err)
	}

	attachRequests, controlRequests, wakeFailedReports := harness.counts()
	if attachRequests != 2 {
		t.Fatalf("expected exactly two attach connections (one forced reconnect), got %d", attachRequests)
	}
	if controlRequests != 1 {
		t.Fatalf(
			"expected exactly one control POST total — the redelivered event.ID must skip straight to a report, not re-storm the control endpoint — got %d",
			controlRequests,
		)
	}
	if wakeFailedReports != 2 {
		t.Fatalf("expected one wake failed report per delivery (2 total, cheap and idempotent), got %d", wakeFailedReports)
	}
}
