package machine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSessionEOFClosesReadersAndJoins(t *testing.T) {
	execStarted := make(chan string)
	gate := make(chan struct{})
	exec := &simpleExecutor{
		start: execStarted,
		gate:  gate,
		handler: func(request protocol.MachineRequest) (protocol.MachineResponse, error) {
			return protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: request.CorrelationID, Operation: request.Operation}, nil
		},
	}

	input := strings.Join([]string{harmonizeRequest(buildReadRequest(1)), harmonizeRequest(buildReadRequest(2))}, "\n")
	out := &bytes.Buffer{}
	session := NewSession(context.Background(), io.NopCloser(strings.NewReader(input)), out, WithExecutor(exec))

	done := make(chan error, 1)
	go func() { done <- session.Run() }()

	readLine(t, execStarted)
	readLine(t, execStarted)
	close(gate)
	if err := readErr(t, done); err != nil {
		t.Fatalf("unexpected session error: %v", err)
	}

	responses := decodeResponses(t, out.Bytes())
	if len(responses) != 2 {
		t.Fatalf("expected two responses, got %d", len(responses))
	}
}

func TestSessionParentCancelClosesInput(t *testing.T) {
	reader := &blockingReadCloser{readStarted: make(chan struct{}, 1), closed: make(chan struct{})}
	exec := &simpleExecutor{handler: successResponse}
	ctx, cancel := context.WithCancel(context.Background())

	session := NewSession(ctx, reader, &bytes.Buffer{}, WithExecutor(exec))
	done := make(chan error, 1)
	go func() { done <- session.Run() }()

	<-reader.readStarted
	cancel()

	if err := readErr(t, done); err != nil {
		t.Fatalf("expected run to stop on parent cancel: %v", err)
	}

	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Fatal("expected blocking reader to be closed on parent cancel")
	}
}

func TestSessionFailingWriterOnAdmissionTerminal(t *testing.T) {
	writer := &bufferedFailWriter{err: io.ErrClosedPipe}
	exec := &simpleExecutor{handler: successResponse}

	lines := make([]string, 0, protocol.MachineMaxCorrelationRegistry+1)
	for i := 0; i < protocol.MachineMaxCorrelationRegistry+1; i++ {
		lines = append(lines, harmonizeRequest(buildReadRequest(i)))
	}

	session := NewSession(context.Background(), io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))), writer, WithExecutor(exec))
	if err := session.Run(); err == nil {
		t.Fatal("expected writer failure during admission terminal")
	}
}

func TestSessionFailingWriterCancelsConcurrentWorkReadPath(t *testing.T) {
	exec := &simpleExecutor{
		start:   make(chan string, 4),
		handler: successResponse,
	}
	writer := &fixedFailWriter{limit: 1}
	in := strings.Join([]string{
		harmonizeRequest(buildReadRequest(1)),
		harmonizeRequest(buildReadRequest(2)),
		harmonizeRequest(buildReadRequest(3)),
	}, "\n")

	session := NewSession(context.Background(), io.NopCloser(strings.NewReader(in)), writer, WithExecutor(exec))
	if err := session.Run(); err == nil {
		t.Fatal("expected writer failure")
	}
	if writer.totalWrites() != 1 {
		t.Fatalf("expected writer to fail on first write attempt, got %d", writer.totalWrites())
	}
}

func TestSessionEOFCancelsBlockingContextAwareExecutor(t *testing.T) {
	execStarted := make(chan string, 1)
	exec := &blockingContextExecutor{started: execStarted}
	out := &bytes.Buffer{}
	session := NewSession(
		context.Background(),
		io.NopCloser(strings.NewReader(harmonizeRequest(buildReadRequest(1)))),
		out,
		WithExecutor(exec),
	)
	if err := session.Run(); err != nil {
		t.Fatalf("expected EOF completion without terminal error: %v", err)
	}
	readLine(t, execStarted)
}

func TestSessionEOFAndCancelRaceGate(t *testing.T) {
	calls := atomic.Int64{}
	exec := &simpleExecutor{handler: func(req protocol.MachineRequest) (protocol.MachineResponse, error) {
		calls.Add(1)
		return successResponse(req)
	}}

	reader := newStagedReadCloser([][]byte{
		[]byte(harmonizeRequest(buildReadRequest(10)) + "\n"),
		[]byte(harmonizeRequest(buildReadRequest(11)) + "\n"),
	}, nil)

	session := NewSession(context.Background(), reader, &bytes.Buffer{}, WithExecutor(exec))
	if err := session.Run(); err != nil {
		t.Fatalf("expected clean run: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two worker calls, got %d", calls.Load())
	}
}

func TestSessionFailingWriterOnEOF(t *testing.T) {
	writer := &bufferedFailWriter{err: errors.New("writer down")}
	exec := &simpleExecutor{
		start:   make(chan string, 2),
		handler: successResponse,
	}
	in := strings.Join([]string{
		harmonizeRequest(buildReadRequest(1)),
		harmonizeRequest(buildReadRequest(2)),
	}, "\n")

	session := NewSession(context.Background(), io.NopCloser(strings.NewReader(in)), writer, WithExecutor(exec))
	done := make(chan error, 1)
	go func() { done <- session.Run() }()

	readLine(t, exec.start)
	if err := readErr(t, done); err == nil {
		t.Fatal("expected writer failure")
	}
	if writer.out.Len() == 0 {
		t.Fatalf("expected at least one response attempt")
	}
}

type blockingContextExecutor struct {
	started chan string
}

func (exec *blockingContextExecutor) Execute(ctx context.Context, request protocol.MachineRequest) (protocol.MachineResponse, error) {
	if exec.started != nil {
		exec.started <- request.CorrelationID
	}
	<-ctx.Done()
	return protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: request.CorrelationID,
		Operation:     request.Operation,
	}, nil
}
