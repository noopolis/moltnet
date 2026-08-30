package machine

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSessionActiveCapacity(t *testing.T) {
	gate := make(chan struct{})
	exec := &simpleExecutor{start: make(chan string, protocol.MachineMaxActiveRequests), gate: gate, handler: successResponse}
	lines := make([]string, 0, protocol.MachineMaxActiveRequests+1)
	for i := 0; i < protocol.MachineMaxActiveRequests+1; i++ {
		lines = append(lines, harmonizeRequest(buildReadRequest(i)))
	}

	out := &responseCaptureWriter{
		records: make(chan protocol.MachineResponse, protocol.MachineMaxActiveRequests+1),
	}
	done := make(chan error, 1)
	session := NewSession(context.Background(), io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))), out, WithExecutor(exec))
	go func() { done <- session.Run() }()

	for i := 0; i < protocol.MachineMaxActiveRequests; i++ {
		readLine(t, exec.start)
	}
	select {
	case response := <-out.records:
		if response.Error == nil || response.Error.Code != protocol.MachineErrorCapacity {
			t.Fatalf("expected active-capacity rejection, got %#v", response)
		}
	case <-waitTimeout(t):
		t.Fatal("timed out waiting for active-capacity rejection")
	}
	close(gate)

	if err := readErr(t, done); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	responses := decodeResponses(t, out.Bytes())
	found := false
	for _, response := range responses {
		if response.Error != nil && response.Error.Code == protocol.MachineErrorCapacity {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected active-capacity rejection")
	}
}

func TestSessionLifetimeCapacityClosesAdmissionAndBoundsState(t *testing.T) {
	exec := &simpleExecutor{handler: successResponse}
	lines := make([]string, 0, protocol.MachineMaxCorrelationRegistry+5)
	for i := 0; i < protocol.MachineMaxCorrelationRegistry+5; i++ {
		lines = append(lines, harmonizeRequest(buildReadRequest(i+1000)))
	}
	out := &bytes.Buffer{}
	session := NewSession(context.Background(), io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))), out, WithExecutor(exec))
	if err := session.Run(); err != nil {
		t.Fatalf("expected session completion: %v", err)
	}

	responses := decodeResponses(t, out.Bytes())
	capacityCount := 0
	for _, response := range responses {
		if response.Error != nil && response.Error.Code == protocol.MachineErrorCapacity {
			capacityCount++
		}
	}
	if capacityCount != 1 {
		t.Fatalf("expected exactly one capacity terminal, got %d", capacityCount)
	}
	if session.lifecycle.size() > protocol.MachineMaxCorrelationRegistry {
		t.Fatalf("expected bounded lifecycle size, got %d", session.lifecycle.size())
	}
}

func TestSessionLifetimeCapacityFailurePropagation(t *testing.T) {
	exec := &simpleExecutor{handler: successResponse}
	writer := &bufferedFailWriter{err: io.ErrClosedPipe}
	lines := make([]string, 0, protocol.MachineMaxCorrelationRegistry+5)
	for i := 0; i < protocol.MachineMaxCorrelationRegistry+5; i++ {
		lines = append(lines, harmonizeRequest(buildReadRequest(i+3000)))
	}

	session := NewSession(context.Background(), io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))), writer, WithExecutor(exec))
	if err := session.Run(); err == nil {
		t.Fatal("expected writer failure on capacity/terminal frame")
	}
	if writer.out.Len() == 0 {
		t.Fatal("expected capacity terminal before writer failure")
	}
}
