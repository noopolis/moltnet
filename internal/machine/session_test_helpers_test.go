package machine

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func harmonizeRequest(request protocol.MachineRequest) string {
	raw, err := protocol.EncodeMachineRequestLine(request)
	if err != nil {
		panic(err)
	}
	return raw
}

func decodeResponses(t *testing.T, raw []byte) []protocol.MachineResponse {
	t.Helper()
	reader := bufio.NewScanner(bytes.NewReader(raw))
	reader.Split(bufio.ScanLines)
	responses := make([]protocol.MachineResponse, 0)
	for reader.Scan() {
		response, err := protocol.DecodeMachineResponseLine(reader.Text())
		if err != nil {
			t.Fatalf("decode response: %v", err)
		}
		responses = append(responses, response)
	}
	return responses
}

func readLine(t *testing.T, ch <-chan string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for executor")
	}
}

func readErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
	return nil
}

func buildReadRequest(index int) protocol.MachineRequest {
	return protocol.MachineRequest{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: fmt.Sprintf("corr_%d", index),
		Operation:     protocol.MachineOpRead,
		Read: &protocol.MachineReadRequest{
			Target: protocol.MachineTarget{Kind: protocol.MachineTargetKindRoom, ID: "room_1"},
			Limit:  1,
		},
	}
}

func buildSendNudgeRequest(index int, body, deliveryID string) protocol.MachineRequest {
	return protocol.MachineRequest{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: fmt.Sprintf("send_%d", index),
		Operation:     protocol.MachineOpSendNudge,
		SendNudge: &protocol.MachineSendNudgeRequest{
			DeliveryID:      deliveryID,
			Target:          protocol.MachineTarget{Kind: protocol.MachineTargetKindRoom, ID: "room_1"},
			Body:            body,
			OriginMessageID: "origin-1",
			CauseEventIDs:   []string{"ev-1"},
		},
	}
}

func successResponse(request protocol.MachineRequest) (protocol.MachineResponse, error) {
	hasMore := false
	accepted := true
	threadCreated := false
	dmCreated := false
	base := protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: request.CorrelationID, Operation: request.Operation}
	if request.Operation == protocol.MachineOpSendNudge {
		base.SendNudge = &protocol.MachineSendNudgeResult{Accepted: &accepted, ThreadCreated: &threadCreated, DMCreated: &dmCreated, MessageID: "msg_1", EventID: "evt_1"}
		return base, nil
	}
	if request.Operation == protocol.MachineOpRead {
		base.Read = &protocol.MachineReadResult{Target: request.Read.Target, Page: protocol.MachineReadPage{Page: protocol.MachineReadPageInfo{HasMore: &hasMore}}}
	}
	return base, nil
}

type trackedDeliveryRegistry struct {
	delegate     DeliveryRegistry
	resolveCalls int
	releaseCalls int
	claims       []DeliveryClaimState
	claimErrs    int
}

func (r *trackedDeliveryRegistry) Claim(identity DeliveryIdentity) (DeliveryClaim, error) {
	claim, err := r.delegate.Claim(identity)
	r.claims = append(r.claims, claim.State)
	if err != nil {
		r.claims = append(r.claims, DeliveryClaimStateCapacity)
		r.claimErrs++
	}
	return claim, err
}

func (r *trackedDeliveryRegistry) Resolve(identity DeliveryIdentity, response protocol.MachineResponse, ambiguous bool) bool {
	r.resolveCalls++
	return r.delegate.Resolve(identity, response, ambiguous)
}

func (r *trackedDeliveryRegistry) Release(identity DeliveryIdentity) bool {
	r.releaseCalls++
	return r.delegate.Release(identity)
}

func (r *trackedDeliveryRegistry) Lookup(identity DeliveryIdentity) (protocol.MachineResponse, bool) {
	return r.delegate.Lookup(identity)
}

func (r *trackedDeliveryRegistry) Size() int {
	return r.delegate.Size()
}

type simpleExecutor struct {
	start              chan string
	gate               chan struct{}
	done               chan string
	ignoreCancellation bool
	handler            func(protocol.MachineRequest) (protocol.MachineResponse, error)
}

func (exec *simpleExecutor) Execute(ctx context.Context, request protocol.MachineRequest) (protocol.MachineResponse, error) {
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
				return protocol.MachineResponse{
					Version:       protocol.MachineProtocolV1,
					CorrelationID: request.CorrelationID,
					Operation:     request.Operation,
				}, ctx.Err()
			case <-exec.gate:
			}
		}
	}
	if exec.handler != nil {
		return exec.handler(request)
	}
	return protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: request.CorrelationID, Operation: request.Operation}, nil
}

type fixedFailWriter struct {
	mu     sync.Mutex
	limit  int
	writes int
	out    bytes.Buffer
}

func (writer *fixedFailWriter) Write(p []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.writes >= writer.limit {
		return 0, io.ErrClosedPipe
	}
	writer.writes++
	return writer.out.Write(p)
}

func (writer *fixedFailWriter) totalWrites() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.writes
}

type bufferedFailWriter struct {
	err error
	out bytes.Buffer
}

func (writer *bufferedFailWriter) Write(p []byte) (int, error) {
	if writer.err == nil {
		return writer.out.Write(p)
	}
	if len(p) == 0 {
		return 0, writer.err
	}
	n, _ := writer.out.Write(p[:1])
	return n, writer.err
}

type blockingReadCloser struct {
	readStarted chan struct{}
	closed      chan struct{}
	once        sync.Once
}

func (reader *blockingReadCloser) Read(p []byte) (int, error) {
	select {
	case reader.readStarted <- struct{}{}:
	default:
	}
	<-reader.closed
	return 0, io.EOF
}

func (reader *blockingReadCloser) Close() error {
	reader.once.Do(func() {
		close(reader.closed)
	})
	return nil
}

type stagedReadCloser struct {
	lines     [][]byte
	pos       int
	nextErr   error
	offset    int
	startGate <-chan struct{}
	closed    bool
	closeOnce sync.Once
}

func newStagedReadCloser(lines [][]byte, startGate <-chan struct{}) *stagedReadCloser {
	return &stagedReadCloser{lines: lines, startGate: startGate}
}

func (reader *stagedReadCloser) Read(p []byte) (int, error) {
	if reader.closed {
		return 0, io.EOF
	}
	if reader.pos >= len(reader.lines) {
		reader.closed = true
		return 0, io.EOF
	}
	if reader.startGate != nil && reader.pos > 0 {
		<-reader.startGate
		reader.startGate = nil
	}

	line := reader.lines[reader.pos]
	n := copy(p, line[reader.offset:])
	reader.offset += n
	if reader.offset >= len(line) {
		reader.pos++
		reader.offset = 0
	}
	return n, reader.nextErr
}

func (reader *stagedReadCloser) Close() error {
	reader.closeOnce.Do(func() {
		reader.closed = true
	})
	return nil
}
