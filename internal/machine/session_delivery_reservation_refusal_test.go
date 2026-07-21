package machine

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSessionDeliveryReservationReleasedBeforeStartWorker(t *testing.T) {
	delivery := &notifiedDeliveryRegistry{
		delegate: &trackedDeliveryRegistry{delegate: newMemoryDeliveryRegistry(16)},
		release:  make(chan DeliveryIdentity, 1),
	}
	calls := atomic.Int64{}
	firstGate := make(chan struct{})
	close(firstGate)

	exec := &simpleExecutor{
		handler: func(req protocol.MachineRequest) (protocol.MachineResponse, error) {
			if req.Operation == protocol.MachineOpSendNudge {
				calls.Add(1)
			}
			return successResponse(req)
		},
	}
	request := buildSendNudgeRequest(1, "hello", "delivery-worker-refusal")
	out := newResponseCaptureWriter()
	session := NewSession(
		context.Background(),
		newStagedReadCloser([][]byte{
			[]byte(harmonizeRequest(request) + "\n"),
		}, firstGate),
		out,
		WithExecutor(exec),
		WithDeliveryRegistry(delivery),
	)
	session.joinMu.Lock()
	session.stopWorkers = true
	session.joinMu.Unlock()

	if err := session.Run(); err != nil {
		t.Fatalf("expected no transport failure on worker refusal: %v", err)
	}

	responses := decodeResponses(t, out.Bytes())
	if len(responses) != 1 {
		t.Fatalf("expected one terminal response, got %d", len(responses))
	}
	if responses[0].Error == nil || responses[0].Error.Code != protocol.MachineErrorTransport {
		t.Fatalf("expected transport terminal from worker refusal, got %+v", responses[0])
	}
	if calls.Load() != 0 {
		t.Fatalf("expected no worker execution when start is refused, got %d", calls.Load())
	}
	requestIdentity, _ := DeliveryIdentityFromSendNudge{}.Identity(request)
	released := <-delivery.release
	if released.Identity != requestIdentity.Identity {
		t.Fatalf("expected release for %q, got %q", requestIdentity.Identity, released.Identity)
	}

	retry := buildSendNudgeRequest(2, "hello", "delivery-worker-refusal")
	retryOut := newResponseCaptureWriter()
	retryCalls := atomic.Int64{}
	retrySessionReader, retrySessionWriter := io.Pipe()
	retrySession := NewSession(
		context.Background(),
		retrySessionReader,
		retryOut,
		WithExecutor(&simpleExecutor{
			handler: func(req protocol.MachineRequest) (protocol.MachineResponse, error) {
				if req.Operation == protocol.MachineOpSendNudge {
					retryCalls.Add(1)
				}
				return successResponse(req)
			},
		}),
		WithDeliveryRegistry(delivery.delegate),
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
		t.Fatalf("expected retry execute after worker refusal: %v", err)
	}
	if retryCalls.Load() != 1 {
		t.Fatalf("expected one retry execution, got %d", retryCalls.Load())
	}
	if len(decodeResponses(t, retryOut.Bytes())) != 1 {
		t.Fatalf("expected retry response, got %d", len(decodeResponses(t, retryOut.Bytes())))
	}
}
