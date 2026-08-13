package machine

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type SessionOption func(*Session)

func WithExecutor(executor Executor) SessionOption {
	return func(session *Session) {
		session.executor = executor
	}
}

func WithDeliveryRegistry(registry DeliveryRegistry) SessionOption {
	return func(session *Session) {
		session.deliveryRegistry = registry
	}
}

func WithDeliveryIdentityResolver(resolver DeliveryIdentityResolver) SessionOption {
	return func(session *Session) {
		session.deliveryResolver = resolver
	}
}

type Session struct {
	ctx              context.Context
	cancel           context.CancelFunc
	input            io.ReadCloser
	reader           *bufio.Reader
	writer           *responseWriter
	executor         Executor
	lifecycle        *requestLifecycleRegistry
	deliveryRegistry DeliveryRegistry
	deliveryResolver DeliveryIdentityResolver

	shutdownOnce      sync.Once
	terminalFailureMu sync.Mutex
	terminalFailure   error
	joinGroup         sync.WaitGroup
	joinMu            sync.Mutex
	stopWorkers       bool
}

func NewSession(ctx context.Context, input io.ReadCloser, writer io.Writer, opts ...SessionOption) *Session {
	session := &Session{
		input:            input,
		writer:           newResponseWriter(writer),
		executor:         noopExecutor{},
		lifecycle:        newRequestLifecycleRegistry(protocol.MachineMaxActiveRequests, protocol.MachineMaxCorrelationRegistry),
		deliveryResolver: DeliveryIdentityFromSendNudge{},
	}
	session.ctx, session.cancel = context.WithCancel(ctx)
	if session.input != nil {
		session.reader = bufio.NewReader(session.input)
	}
	for _, option := range opts {
		option(session)
	}
	if session.deliveryRegistry == nil {
		session.deliveryRegistry = newMemoryDeliveryRegistry(protocol.MachineMaxDeliveryRegistry)
	}
	if session.executor == nil {
		session.executor = noopExecutor{}
	}
	return session
}

func (session *Session) Run() error {
	if session.input == nil {
		return errSessionInputMissing
	}

	parentDone := make(chan struct{})
	var parentWatcher sync.WaitGroup
	parentWatcher.Add(1)
	go func() {
		defer parentWatcher.Done()
		session.watchParent(parentDone)
	}()
	defer func() {
		close(parentDone)
		parentWatcher.Wait()
	}()

	var runErr error

readLoop:
	for {
		if session.isShuttingDownOrStopped() || session.ctx.Err() != nil {
			break
		}

		rawLine, readErr := session.readPhysicalLine()
		switch {
		case errors.Is(readErr, io.EOF):
			if len(rawLine) > 0 {
				err := session.processRawLine(rawLine)
				if err != nil && runErr == nil {
					runErr = err
				}
			}
			session.shutdown(nil)
		case readErr == nil:
			err := session.processRawLine(rawLine)
			if err != nil {
				runErr = err
				break readLoop
			}
		case errors.Is(readErr, context.Canceled) || errors.Is(session.ctx.Err(), context.Canceled):
			break readLoop
		default:
			if runErr == nil {
				runErr = readErr
			}
			session.shutdown(readErr)
		}

		if readErr != nil {
			break
		}
	}

	session.shutdown(nil)
	session.joinMu.Lock()
	session.stopWorkers = true
	session.joinMu.Unlock()
	session.joinGroup.Wait()

	if runErr != nil {
		return runErr
	}
	session.terminalFailureMu.Lock()
	terr := session.terminalFailure
	session.terminalFailureMu.Unlock()
	return terr
}

func (session *Session) processRawLine(rawLine []byte) error {
	line := bytes.TrimSuffix(rawLine, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 {
		session.shutdown(errMalformedLine)
		return errMalformedLine
	}

	request, decodeErr := protocol.DecodeMachineRequestLine(string(line))
	if decodeErr != nil {
		session.shutdown(errMalformedRequest)
		return decodeErr
	}
	session.handleRequest(request)
	return nil
}

func (session *Session) onWriterFailure(err error) {
	if err == nil {
		return
	}
	session.setTerminalFailure(err)
	if !session.isShuttingDownOrStopped() {
		session.shutdown(err)
	}
}

func (session *Session) setTerminalFailure(err error) {
	if err == nil {
		return
	}
	session.terminalFailureMu.Lock()
	if session.terminalFailure == nil {
		session.terminalFailure = err
	}
	session.terminalFailureMu.Unlock()
}

func (session *Session) shutdown(err error) {
	session.joinMu.Lock()
	session.stopWorkers = true
	session.joinMu.Unlock()

	session.shutdownOnce.Do(func() {
		if err != nil && !errors.Is(err, context.Canceled) {
			session.setTerminalFailure(err)
		}
		targets, _ := session.lifecycle.closeAdmission()
		session.cancel()
		if session.input != nil {
			_ = session.input.Close()
		}
		for _, target := range targets {
			if target.cancel != nil {
				target.cancel()
			}
			session.writeResponseNoClaim(terminalCanceledResponse(target.correlation, target.operation))
		}
	})
}

func (session *Session) isShuttingDownOrStopped() bool {
	return session.lifecycle.isShuttingDown()
}

func (session *Session) watchParent(done <-chan struct{}) {
	select {
	case <-session.ctx.Done():
		session.shutdown(nil)
	case <-done:
	}
}

func (session *Session) readPhysicalLine() ([]byte, error) {
	const max = protocol.MachineMaxInputLineBytes
	line := make([]byte, 0, 128)
	for {
		ch, err := session.reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return line, io.EOF
			}
			return line, err
		}
		if len(line) >= max {
			return line, errPhysicalLineTooLong
		}
		line = append(line, ch)
		if ch == '\n' {
			return line, nil
		}
	}
}

func (session *Session) writeResponse(response protocol.MachineResponse) {
	if err := session.writer.write(response); err != nil {
		session.onWriterFailure(err)
	}
}

func (session *Session) writeResponseNoClaim(response protocol.MachineResponse) {
	if response.CorrelationID == "" {
		return
	}
	if err := session.writer.write(response); err != nil {
		session.onWriterFailure(err)
	}
}

var errPhysicalLineTooLong = errors.New("request line exceeds maximum bytes")
var errMalformedLine = errors.New("malformed request line")
var errMalformedRequest = errors.New("malformed request")
var errSessionInputMissing = errors.New("session input must be an owned io.ReadCloser")
