package machine

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSessionCancelWinsOverWorker(t *testing.T) {
	exec := &simpleExecutor{
		start:   make(chan string, 1),
		gate:    make(chan struct{}),
		handler: successResponse,
	}
	out := &bytes.Buffer{}
	in := strings.Join([]string{
		harmonizeRequest(buildReadRequest(1)),
		harmonizeRequest(protocol.MachineRequest{
			Version:       protocol.MachineProtocolV1,
			CorrelationID: "cancel_1",
			Operation:     protocol.MachineOpCancel,
			Cancel:        &protocol.MachineCancelRequest{TargetCorrelationID: "corr_1"},
		}),
	}, "\n")

	session := NewSession(context.Background(), io.NopCloser(strings.NewReader(in)), out, WithExecutor(exec))
	done := make(chan error, 1)
	go func() { done <- session.Run() }()

	readLine(t, exec.start)
	if err := readErr(t, done); err != nil {
		t.Fatalf("session failed: %v", err)
	}

	responses := decodeResponses(t, out.Bytes())
	if len(responses) != 2 {
		t.Fatalf("expected two terminal responses, got %d", len(responses))
	}
	found := map[string]bool{}
	for _, response := range responses {
		found[response.CorrelationID+"-"+response.Operation] = true
	}
	if _, ok := found["corr_1-read"]; !ok {
		t.Fatalf("expected canceled terminal for target correlation: %+v", responses)
	}
	if _, ok := found["cancel_1-cancel"]; !ok {
		t.Fatalf("expected cancel request terminal: %+v", responses)
	}
	for _, response := range responses {
		if response.Operation == protocol.MachineOpRead {
			if response.Error == nil || response.Error.Code != protocol.MachineErrorCanceled {
				t.Fatalf("expected read canceled terminal, got %#v", response)
			}
		}
		if response.Operation == protocol.MachineOpCancel {
			if response.Cancel == nil || response.Cancel.State != protocol.MachineCancelStateCanceled {
				t.Fatalf("expected cancel state %q, got %#v", protocol.MachineCancelStateCanceled, response.Cancel)
			}
		}
	}
}

func TestSessionCompletionWinsOverCancel(t *testing.T) {
	execDone := make(chan struct{})
	exec := &simpleExecutor{
		handler: func(req protocol.MachineRequest) (protocol.MachineResponse, error) {
			close(execDone)
			return successResponse(req)
		},
	}

	reader := newStagedReadCloser([][]byte{
		[]byte(harmonizeRequest(buildReadRequest(2)) + "\n"),
		[]byte(harmonizeRequest(protocol.MachineRequest{
			Version:       protocol.MachineProtocolV1,
			CorrelationID: "cancel_2",
			Operation:     protocol.MachineOpCancel,
			Cancel:        &protocol.MachineCancelRequest{TargetCorrelationID: "corr_2"},
		}) + "\n"),
	}, execDone)

	out := &bytes.Buffer{}
	session := NewSession(context.Background(), reader, out, WithExecutor(exec))
	if err := session.Run(); err != nil {
		t.Fatalf("session failed: %v", err)
	}

	responses := decodeResponses(t, out.Bytes())
	if len(responses) != 2 {
		t.Fatalf("expected target terminal and cancel terminal, got %d", len(responses))
	}
	if responses[0].Operation != protocol.MachineOpRead || responses[0].Error != nil {
		t.Fatalf("expected read terminal success first, got %+v", responses[0])
	}
	if responses[1].Cancel == nil || responses[1].Cancel.State != protocol.MachineCancelStateAlreadyFinal {
		t.Fatalf("expected cancel state already_final, got %+v", responses[1])
	}
}

func TestSessionCancelRepeatedOnlyOneTargetTerminal(t *testing.T) {
	targetRequest := buildReadRequest(3)
	firstCancel := protocol.MachineRequest{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: "cancel_3_a",
		Operation:     protocol.MachineOpCancel,
		Cancel:        &protocol.MachineCancelRequest{TargetCorrelationID: targetRequest.CorrelationID},
	}
	secondCancel := protocol.MachineRequest{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: "cancel_3_b",
		Operation:     protocol.MachineOpCancel,
		Cancel:        &protocol.MachineCancelRequest{TargetCorrelationID: targetRequest.CorrelationID},
	}
	exec := &simpleExecutor{
		start:              make(chan string, 1),
		gate:               make(chan struct{}),
		ignoreCancellation: true,
		handler:            successResponse,
	}
	out := newResponseCaptureWriter()

	reader, writer := io.Pipe()
	session := NewSession(context.Background(), reader, out, WithExecutor(exec))
	done := make(chan error, 1)
	go func() {
		done <- session.Run()
	}()

	if _, err := writer.Write([]byte(harmonizeRequest(targetRequest) + "\n")); err != nil {
		t.Fatalf("expected read request write: %v", err)
	}
	<-exec.start
	if _, err := writer.Write([]byte(harmonizeRequest(firstCancel) + "\n")); err != nil {
		t.Fatalf("expected first cancel write: %v", err)
	}
	if _, err := writer.Write([]byte(harmonizeRequest(secondCancel) + "\n")); err != nil {
		t.Fatalf("expected second cancel write: %v", err)
	}

	seenTarget := false
	seenFirstCancel := false
	seenSecondCancel := false
	for !seenTarget || !seenFirstCancel || !seenSecondCancel {
		response := waitResponseByCorrelation(out, func(response protocol.MachineResponse) bool {
			return response.CorrelationID == targetRequest.CorrelationID ||
				response.CorrelationID == firstCancel.CorrelationID ||
				response.CorrelationID == secondCancel.CorrelationID
		})
		switch response.CorrelationID {
		case targetRequest.CorrelationID:
			if seenTarget {
				t.Fatalf("expected one target terminal for %q, got duplicate in %v", targetRequest.CorrelationID, response)
			}
			if response.Error == nil || response.Error.Code != protocol.MachineErrorCanceled {
				t.Fatalf("expected target canceled error for %q, got %#v", targetRequest.CorrelationID, response)
			}
			seenTarget = true
		case firstCancel.CorrelationID:
			if seenFirstCancel {
				t.Fatalf("expected one first-cancel terminal for %q, got duplicate in %v", firstCancel.CorrelationID, response)
			}
			if response.Cancel == nil || response.Cancel.State != protocol.MachineCancelStateCanceled || response.Cancel.TargetCorrelationID != targetRequest.CorrelationID {
				t.Fatalf("expected first cancel state %q for target %q, got %#v", protocol.MachineCancelStateCanceled, targetRequest.CorrelationID, response)
			}
			seenFirstCancel = true
		case secondCancel.CorrelationID:
			if seenSecondCancel {
				t.Fatalf("expected one second-cancel terminal for %q, got duplicate in %v", secondCancel.CorrelationID, response)
			}
			if response.Cancel == nil || response.Cancel.State != protocol.MachineCancelStateAlreadyFinal || response.Cancel.TargetCorrelationID != targetRequest.CorrelationID {
				t.Fatalf("expected second cancel state %q for target %q, got %#v", protocol.MachineCancelStateAlreadyFinal, targetRequest.CorrelationID, response)
			}
			seenSecondCancel = true
		default:
			t.Fatalf("unexpected terminal response in repeated cancel proof: %#v", response)
		}
	}

	close(exec.gate)
	if err := writer.Close(); err != nil {
		t.Fatalf("expected input close: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("expected clean run: %v", err)
	}

	responses := decodeResponses(t, out.Bytes())
	if len(responses) != 3 {
		t.Fatalf("expected three terminal responses, got %d in %v", len(responses), responses)
	}

	correlations := map[string]int{
		targetRequest.CorrelationID: 0,
		firstCancel.CorrelationID:   0,
		secondCancel.CorrelationID:  0,
	}
	for _, response := range responses {
		if response.CorrelationID == targetRequest.CorrelationID {
			if response.Error == nil || response.Error.Code != protocol.MachineErrorCanceled {
				t.Fatalf("expected read terminal canceled error for %q, got %#v", targetRequest.CorrelationID, response)
			}
		}
		if response.CorrelationID == firstCancel.CorrelationID {
			if response.Cancel == nil || response.Cancel.State != protocol.MachineCancelStateCanceled {
				t.Fatalf("expected first cancel terminal %q: %#v", firstCancel.CorrelationID, response)
			}
		}
		if response.CorrelationID == secondCancel.CorrelationID {
			if response.Cancel == nil || response.Cancel.State != protocol.MachineCancelStateAlreadyFinal {
				t.Fatalf("expected second cancel terminal %q: %#v", secondCancel.CorrelationID, response)
			}
		}
		count, ok := correlations[response.CorrelationID]
		if !ok {
			t.Fatalf("unexpected correlation in repeated cancel proof: %s", response.CorrelationID)
		}
		correlations[response.CorrelationID] = count + 1
	}
	if len(correlations) != 3 || correlations[targetRequest.CorrelationID] != 1 || correlations[firstCancel.CorrelationID] != 1 || correlations[secondCancel.CorrelationID] != 1 {
		t.Fatalf("expected one terminal per correlation for repeated cancel proof, got %v", correlations)
	}
}

func TestSessionCancelWinsWithConcurrentWorker(t *testing.T) {
	exec := &simpleExecutor{
		start:   make(chan string, 1),
		gate:    make(chan struct{}),
		handler: successResponse,
	}
	out := newResponseCaptureWriter()
	reader, writer := io.Pipe()
	session := NewSession(context.Background(), reader, out, WithExecutor(exec))
	done := make(chan error, 1)
	go func() {
		done <- session.Run()
	}()
	targetRequest := buildReadRequest(4)
	cancelRequest := protocol.MachineRequest{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: "cancel_4",
		Operation:     protocol.MachineOpCancel,
		Cancel:        &protocol.MachineCancelRequest{TargetCorrelationID: "corr_4"},
	}

	if _, err := writer.Write([]byte(harmonizeRequest(targetRequest) + "\n")); err != nil {
		t.Fatalf("expected read request write: %v", err)
	}
	<-exec.start
	if _, err := writer.Write([]byte(harmonizeRequest(cancelRequest) + "\n")); err != nil {
		t.Fatalf("expected cancel request write: %v", err)
	}

	seenTarget := false
	seenCancel := false
	for !seenTarget || !seenCancel {
		response := waitResponseByCorrelation(out, func(response protocol.MachineResponse) bool {
			if response.CorrelationID == targetRequest.CorrelationID {
				return response.Error != nil && response.Error.Code == protocol.MachineErrorCanceled
			}
			if response.CorrelationID == cancelRequest.CorrelationID {
				return response.Cancel != nil && response.Cancel.State == protocol.MachineCancelStateCanceled
			}
			return false
		})
		switch response.CorrelationID {
		case targetRequest.CorrelationID:
			seenTarget = true
		case cancelRequest.CorrelationID:
			seenCancel = true
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("expected input close: %v", err)
	}

	if err := readErr(t, done); err != nil {
		t.Fatalf("expected clean run: %v", err)
	}

	responses := decodeResponses(t, out.Bytes())
	if len(responses) != 2 {
		t.Fatalf("expected two terminal responses, got %d", len(responses))
	}
	found := map[string]bool{}
	for _, response := range responses {
		found[response.CorrelationID] = true
		if response.CorrelationID == cancelRequest.CorrelationID {
			if response.Cancel == nil || response.Cancel.State != protocol.MachineCancelStateCanceled {
				t.Fatalf("expected canceled state, got %+v", response)
			}
		}
		if response.CorrelationID == targetRequest.CorrelationID {
			if response.Error == nil || response.Error.Code != protocol.MachineErrorCanceled {
				t.Fatalf("expected target canceled, got %+v", response)
			}
		}
	}
	if !found[targetRequest.CorrelationID] {
		t.Fatalf("expected target correlation %q in responses, got %v", targetRequest.CorrelationID, responses)
	}
	if !found[cancelRequest.CorrelationID] {
		t.Fatalf("expected cancel correlation %q in responses, got %v", cancelRequest.CorrelationID, responses)
	}
	if len(found) != 2 {
		t.Fatalf("expected exactly two correlations, got %v", found)
	}
}

func TestSessionCancelSelfCancel(t *testing.T) {
	out := newResponseCaptureWriter()
	exec := &simpleExecutor{handler: successResponse}
	reader, writer := io.Pipe()
	session := NewSession(context.Background(), reader, out, WithExecutor(exec))
	done := make(chan error, 1)
	go func() {
		done <- session.Run()
	}()

	cancel := protocol.MachineRequest{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: "cancel_self",
		Operation:     protocol.MachineOpCancel,
		Cancel:        &protocol.MachineCancelRequest{TargetCorrelationID: "cancel_self"},
	}
	if _, err := writer.Write([]byte(harmonizeRequest(cancel) + "\n")); err != nil {
		t.Fatalf("expected self-cancel request write: %v", err)
	}
	response := waitResponseByCorrelation(out, func(response protocol.MachineResponse) bool {
		return response.CorrelationID == cancel.CorrelationID
	})
	if response.Operation != protocol.MachineOpCancel {
		t.Fatalf("expected cancel operation, got %q", response.Operation)
	}
	if response.Error != nil {
		t.Fatalf("expected no error on self-cancel, got %v", response.Error)
	}
	if response.Cancel == nil {
		t.Fatalf("expected cancel result")
	}
	if response.Cancel.TargetCorrelationID != cancel.CorrelationID {
		t.Fatalf("expected target correlation %q, got %q", cancel.CorrelationID, response.Cancel.TargetCorrelationID)
	}
	if response.Cancel.State != protocol.MachineCancelStateCanceled {
		t.Fatalf("expected cancel state %q, got %q", protocol.MachineCancelStateCanceled, response.Cancel.State)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("expected input close: %v", err)
	}
	if err := readErr(t, done); err != nil {
		t.Fatalf("expected clean run: %v", err)
	}
	responses := decodeResponses(t, out.Bytes())
	if len(responses) != 1 {
		t.Fatalf("expected one terminal response, got %d", len(responses))
	}
	if responses[0].CorrelationID != cancel.CorrelationID {
		t.Fatalf("expected correlation %q, got %q", cancel.CorrelationID, responses[0].CorrelationID)
	}
	if responses[0].Cancel == nil || responses[0].Cancel.State != protocol.MachineCancelStateCanceled {
		t.Fatalf("expected one self-cancel terminal, got %+v", responses[0])
	}
	if responses[0].Cancel.TargetCorrelationID != cancel.CorrelationID {
		t.Fatalf("expected self target %q, got %q", cancel.CorrelationID, responses[0].Cancel.TargetCorrelationID)
	}
	if responses[0].Error != nil {
		t.Fatalf("expected no error in self-cancel terminal, got %v", responses[0].Error)
	}
}
