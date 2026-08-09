package machine

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type responseCaptureWriter struct {
	mu      sync.Mutex
	out     bytes.Buffer
	records chan protocol.MachineResponse
}

func newResponseCaptureWriter() *responseCaptureWriter {
	return &responseCaptureWriter{
		records: make(chan protocol.MachineResponse, 8),
	}
}

func (writer *responseCaptureWriter) Write(p []byte) (int, error) {
	writer.mu.Lock()
	n, err := writer.out.Write(p)
	writer.mu.Unlock()

	line := bytes.TrimSpace(p)
	if len(line) == 0 {
		return n, nil
	}
	if response, decodeErr := protocol.DecodeMachineResponseLine(string(line)); decodeErr == nil {
		writer.records <- response
	}
	return n, err
}

func (writer *responseCaptureWriter) Bytes() []byte {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.out.Bytes()
}

type countingExecutor struct {
	start              chan string
	gate               chan struct{}
	done               chan string
	calls              *atomic.Int64
	errorf             error
	ignoreCancellation bool
}

func (exec *countingExecutor) Execute(ctx context.Context, request protocol.MachineRequest) (protocol.MachineResponse, error) {
	if exec.calls != nil {
		exec.calls.Add(1)
	}
	if exec.start != nil {
		exec.start <- request.CorrelationID
	}
	if exec.done != nil {
		defer func() {
			exec.done <- request.CorrelationID
		}()
	}
	if exec.gate != nil {
		if exec.ignoreCancellation {
			<-exec.gate
		} else {
			select {
			case <-ctx.Done():
			case <-exec.gate:
			}
		}
	}
	response, err := successResponse(request)
	if exec.errorf != nil {
		return response, exec.errorf
	}
	return response, err
}

func waitResponseByCorrelation(writer *responseCaptureWriter, fn func(response protocol.MachineResponse) bool) protocol.MachineResponse {
	for {
		response := <-writer.records
		if fn(response) {
			return response
		}
	}
}

type notifiedDeliveryRegistry struct {
	delegate *trackedDeliveryRegistry
	release  chan DeliveryIdentity
}

func (registry *notifiedDeliveryRegistry) Claim(identity DeliveryIdentity) (DeliveryClaim, error) {
	return registry.delegate.Claim(identity)
}

func (registry *notifiedDeliveryRegistry) Resolve(identity DeliveryIdentity, response protocol.MachineResponse, ambiguous bool) bool {
	return registry.delegate.Resolve(identity, response, ambiguous)
}

func (registry *notifiedDeliveryRegistry) Release(identity DeliveryIdentity) bool {
	released := registry.delegate.Release(identity)
	if released {
		registry.release <- identity
	}
	return released
}

func (registry *notifiedDeliveryRegistry) Lookup(identity DeliveryIdentity) (protocol.MachineResponse, bool) {
	return registry.delegate.Lookup(identity)
}

func (registry *notifiedDeliveryRegistry) Size() int {
	return registry.delegate.Size()
}

func TestSessionDeliveryResolveAmbiguousAfterLateCancel(t *testing.T) {
	delivery := &trackedDeliveryRegistry{delegate: newMemoryDeliveryRegistry(16)}
	start := make(chan string, 1)
	hold := make(chan struct{})
	execDone := make(chan string, 2)
	calls := atomic.Int64{}

	out := newResponseCaptureWriter()
	first := buildSendNudgeRequest(1, "hello", "delivery-ambig")
	cancel := protocol.MachineRequest{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: "cancel_ambig",
		Operation:     protocol.MachineOpCancel,
		Cancel:        &protocol.MachineCancelRequest{TargetCorrelationID: first.CorrelationID},
	}

	exec := &countingExecutor{
		start:              start,
		gate:               hold,
		done:               execDone,
		calls:              &calls,
		ignoreCancellation: true,
	}

	firstSessionGate := make(chan struct{})
	firstSessionReader := newStagedReadCloser([][]byte{
		[]byte(harmonizeRequest(first) + "\n"),
		[]byte(harmonizeRequest(cancel) + "\n"),
	}, firstSessionGate)

	session := NewSession(context.Background(), firstSessionReader, out, WithExecutor(exec), WithDeliveryRegistry(delivery))
	done := make(chan error, 1)
	go func() {
		done <- session.Run()
	}()

	<-start
	close(firstSessionGate)
	targetSeen := false
	cancelSeen := false
	for !targetSeen || !cancelSeen {
		response := waitResponseByCorrelation(out, func(response protocol.MachineResponse) bool {
			if response.CorrelationID == first.CorrelationID {
				return response.Error != nil && response.Error.Code == protocol.MachineErrorCanceled
			}
			if response.CorrelationID == cancel.CorrelationID {
				return response.Cancel != nil && response.Cancel.State == protocol.MachineCancelStateCanceled
			}
			return false
		})
		switch response.CorrelationID {
		case first.CorrelationID:
			targetSeen = true
		case cancel.CorrelationID:
			cancelSeen = true
		}
	}

	close(hold)
	<-execDone
	if err := <-done; err != nil {
		t.Fatalf("expected clean run: %v", err)
	}

	responses := decodeResponses(t, out.Bytes())
	if calls.Load() != 1 {
		t.Fatalf("expected one execution for canceled path, got %d", calls.Load())
	}
	if len(responses) != 2 {
		t.Fatalf("expected two terminal responses, got %d", len(responses))
	}
	foundCancelTerminal := false
	foundCancelResult := false
	for _, response := range responses {
		if response.CorrelationID == first.CorrelationID {
			if response.Error == nil || response.Error.Code != protocol.MachineErrorCanceled {
				t.Fatalf("expected canceled terminal for first correlation, got %+v", response)
			}
			foundCancelTerminal = true
		}
		if response.CorrelationID == cancel.CorrelationID {
			if response.Cancel == nil || response.Cancel.State != protocol.MachineCancelStateCanceled {
				t.Fatalf("expected canceled cancel result for %q, got %+v", cancel.CorrelationID, response)
			}
			foundCancelResult = true
		}
	}
	if !foundCancelTerminal || !foundCancelResult {
		t.Fatalf("expected one canceled terminal and one cancel result for first session: %v", responses)
	}

	retry := buildSendNudgeRequest(2, "hello", "delivery-ambig")
	retryOut := newResponseCaptureWriter()
	retryCalls := atomic.Int64{}
	retryExec := &simpleExecutor{
		handler: func(request protocol.MachineRequest) (protocol.MachineResponse, error) {
			if request.Operation == protocol.MachineOpSendNudge {
				retryCalls.Add(1)
			}
			return successResponse(request)
		},
	}
	retrySession := NewSession(
		context.Background(),
		io.NopCloser(strings.NewReader(harmonizeRequest(retry))),
		retryOut,
		WithExecutor(retryExec),
		WithDeliveryRegistry(delivery),
	)
	if err := retrySession.Run(); err != nil {
		t.Fatalf("expected retry session success: %v", err)
	}
	cached := decodeResponses(t, retryOut.Bytes())
	if len(cached) != 1 {
		t.Fatalf("expected cached retry terminal, got %d", len(cached))
	}
	if cached[0].CorrelationID != retry.CorrelationID {
		t.Fatalf("expected retry correlation %q, got %q", retry.CorrelationID, cached[0].CorrelationID)
	}
	if cached[0].Error == nil || cached[0].Error.Code != protocol.MachineErrorTransport {
		t.Fatalf("expected retry cached transport, got %+v", cached[0])
	}
	if calls.Load()+retryCalls.Load() != 1 {
		t.Fatalf("expected one semantic execution across cancel + retry, got %d", calls.Load()+retryCalls.Load())
	}
}

func TestSessionDeliveryReservationReleasedAfterActiveCapacity(t *testing.T) {
	execStarted := make(chan string, 2)
	holding := make(chan struct{})
	firstDone := make(chan string, 2)
	calls := atomic.Int64{}
	release := make(chan DeliveryIdentity, 1)
	delivery := &notifiedDeliveryRegistry{
		delegate: &trackedDeliveryRegistry{delegate: newMemoryDeliveryRegistry(16)},
		release:  release,
	}
	exec := &simpleExecutor{
		start: execStarted,
		gate:  holding,
		done:  firstDone,
		handler: func(req protocol.MachineRequest) (protocol.MachineResponse, error) {
			if req.Operation == protocol.MachineOpSendNudge {
				calls.Add(1)
			}
			return successResponse(req)
		},
	}

	out := newResponseCaptureWriter()
	requestOne := buildSendNudgeRequest(1, "hello", "delivery-capacity-a")
	requestTwo := buildSendNudgeRequest(2, "hello", "delivery-capacity-b")
	firstSessionReader, firstSessionWriter := io.Pipe()
	first := NewSession(context.Background(), firstSessionReader, out, WithExecutor(exec), WithDeliveryRegistry(delivery))
	first.lifecycle = newRequestLifecycleRegistry(1, protocol.MachineMaxCorrelationRegistry)
	done := make(chan error, 1)
	go func() {
		done <- first.Run()
	}()

	if _, err := firstSessionWriter.Write([]byte(harmonizeRequest(requestOne) + "\n")); err != nil {
		t.Fatalf("expected request one write: %v", err)
	}
	<-execStarted
	if _, err := firstSessionWriter.Write([]byte(harmonizeRequest(requestTwo) + "\n")); err != nil {
		t.Fatalf("expected request two write: %v", err)
	}
	identityB, _ := DeliveryIdentityFromSendNudge{}.Identity(requestTwo)
	released := <-release
	if released != identityB {
		t.Fatalf("expected release for %q, got %q", identityB.Identity, released.Identity)
	}
	_ = waitResponseByCorrelation(out, func(response protocol.MachineResponse) bool {
		return response.CorrelationID == requestTwo.CorrelationID &&
			response.Error != nil &&
			response.Error.Code == protocol.MachineErrorCapacity
	})

	close(holding)
	_ = waitResponseByCorrelation(out, func(response protocol.MachineResponse) bool {
		return response.CorrelationID == requestOne.CorrelationID &&
			response.Error == nil &&
			response.SendNudge != nil &&
			response.SendNudge.Accepted != nil &&
			*response.SendNudge.Accepted
	})
	if err := firstSessionWriter.Close(); err != nil {
		t.Fatalf("expected request input close: %v", err)
	}
	<-firstDone
	if err := <-done; err != nil {
		t.Fatalf("expected clean run: %v", err)
	}

	retry := buildSendNudgeRequest(3, "hello", "delivery-capacity-b")
	retryOut := newResponseCaptureWriter()
	retryCalls := atomic.Int64{}
	retryExec := &simpleExecutor{
		done: make(chan string, 1),
		handler: func(req protocol.MachineRequest) (protocol.MachineResponse, error) {
			if req.Operation == protocol.MachineOpSendNudge {
				retryCalls.Add(1)
			}
			return successResponse(req)
		},
	}
	retrySessionReader, retrySessionWriter := io.Pipe()
	retrySession := NewSession(
		context.Background(),
		retrySessionReader,
		retryOut,
		WithExecutor(retryExec),
		WithDeliveryRegistry(delivery),
	)
	retryDone := make(chan error, 1)
	go func() {
		retryDone <- retrySession.Run()
	}()
	if _, err := retrySessionWriter.Write([]byte(harmonizeRequest(retry) + "\n")); err != nil {
		t.Fatalf("expected retry request write: %v", err)
	}
	_ = waitResponseByCorrelation(retryOut, func(response protocol.MachineResponse) bool {
		return response.CorrelationID == retry.CorrelationID &&
			response.Error == nil &&
			response.SendNudge != nil &&
			response.SendNudge.Accepted != nil &&
			*response.SendNudge.Accepted
	})
	if err := retrySessionWriter.Close(); err != nil {
		t.Fatalf("expected retry input close: %v", err)
	}
	if err := <-retryDone; err != nil {
		t.Fatalf("expected retry to execute after release: %v", err)
	}
	if retryCalls.Load() != 1 {
		t.Fatalf("expected retry execution, got %d", retryCalls.Load())
	}
	retryClaims := delivery.delegate.claims
	if len(retryClaims) != 3 {
		t.Fatalf("expected three delivery claims, got %d", len(retryClaims))
	}
	for _, claim := range retryClaims {
		if claim != DeliveryClaimStateNew {
			t.Fatalf("expected delivery claims to be New, got %v", claim)
		}
	}
}
