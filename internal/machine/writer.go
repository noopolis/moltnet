package machine

import (
	"io"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type responseWriter struct {
	mu     sync.Mutex
	out    io.Writer
	failed error
}

func newResponseWriter(out io.Writer) *responseWriter {
	return &responseWriter{out: out}
}

func (writer *responseWriter) write(response protocol.MachineResponse) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.failed != nil {
		return writer.failed
	}

	line, err := protocol.EncodeMachineResponseLine(response)
	if err != nil {
		return err
	}
	payload := []byte(line + "\n")
	for len(payload) > 0 {
		n, writeErr := writer.out.Write(payload)
		if writeErr != nil {
			writer.failed = writeErr
			return writeErr
		}
		if n == 0 {
			writer.failed = io.ErrShortWrite
			return writer.failed
		}
		payload = payload[n:]
	}
	return nil
}
