package machine

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type boundedWriter struct {
	base      io.Writer
	chunkSize int
}

func (writer boundedWriter) Write(p []byte) (int, error) {
	if writer.chunkSize <= 0 {
		return 0, io.ErrShortWrite
	}
	if len(p) <= writer.chunkSize {
		return writer.base.Write(p)
	}
	return writer.base.Write(p[:writer.chunkSize])
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write(p []byte) (int, error) {
	return 0, writer.err
}

func TestResponseWriterShortWrites(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	out := &responseWriter{
		out: boundedWriter{
			base:      buf,
			chunkSize: 2,
		},
	}
	response := protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: "corr_1",
		Operation:     protocol.MachineOpRead,
		Error: &protocol.MachineError{
			Code: protocol.MachineErrorUnsupported,
		},
	}
	if err := out.write(response); err != nil {
		t.Fatalf("writer failed on short writes: %v", err)
	}
	decoded, decodeErr := protocol.DecodeMachineResponseLine(buf.String())
	if decodeErr != nil {
		t.Fatalf("decoded short-write payload does not parse: %v", decodeErr)
	}
	if decoded.Error.Code != protocol.MachineErrorUnsupported {
		t.Fatalf("unexpected code %q", decoded.Error.Code)
	}
}

func TestResponseWriterFailsOnFirstError(t *testing.T) {
	t.Parallel()

	out := &responseWriter{
		out: failingWriter{err: errors.New("writer down")},
	}
	response := protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: "corr_1",
		Operation:     protocol.MachineOpRead,
		Error: &protocol.MachineError{
			Code: protocol.MachineErrorTransport,
		},
	}
	if err := out.write(response); err == nil {
		t.Fatal("expected writer failure")
	}
}
