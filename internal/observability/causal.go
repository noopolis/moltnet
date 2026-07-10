package observability

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// CausalWriter is an append-only per-network causal JSONL sink. It owns
// sequence assignment for every (run_id, stream_id) it sees, so callers never
// have to coordinate contiguity themselves: Append fills in event.Emitter.Seq
// before validating and writing the record.
type CausalWriter struct {
	mu     sync.Mutex
	writer io.Writer
	seqs   map[causalStreamKey]int
}

type causalStreamKey struct {
	runID    string
	streamID string
}

// NewCausalFileWriter opens (creating parent directories and the file as
// needed) an append-only JSONL file at path for causal events.
func NewCausalFileWriter(path string) (*CausalWriter, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("causal writer: path is required")
	}
	if dir := filepath.Dir(trimmed); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("causal writer: create directory: %w", err)
		}
	}
	file, err := os.OpenFile(trimmed, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("causal writer: open %q: %w", trimmed, err)
	}
	return NewCausalWriter(file), nil
}

// NewCausalWriter wraps an arbitrary io.Writer (a file, an in-memory buffer
// in tests, etc.) as a CausalWriter.
func NewCausalWriter(writer io.Writer) *CausalWriter {
	return &CausalWriter{
		writer: writer,
		seqs:   make(map[causalStreamKey]int),
	}
}

// Append assigns the next contiguous seq for (event.RunID,
// event.Emitter.StreamID), validates the resulting record, and appends it as
// one JSONL line. It returns the stamped event (with Emitter.Seq populated)
// regardless of whether the append itself succeeded, so callers can log the
// attempted record on failure.
func (w *CausalWriter) Append(event protocol.CausalEvent) (protocol.CausalEvent, error) {
	if w == nil {
		return event, fmt.Errorf("causal writer: writer is nil")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	key := causalStreamKey{runID: event.RunID, streamID: event.Emitter.StreamID}
	event.Emitter.Seq = w.seqs[key] + 1

	if err := event.Validate(); err != nil {
		return event, fmt.Errorf("causal writer: %w", err)
	}

	line, err := json.Marshal(event)
	if err != nil {
		return event, fmt.Errorf("causal writer: encode event: %w", err)
	}
	line = append(line, '\n')
	if _, err := w.writer.Write(line); err != nil {
		return event, fmt.Errorf("causal writer: write event: %w", err)
	}

	w.seqs[key] = event.Emitter.Seq
	return event, nil
}

// Close releases the underlying writer if it supports io.Closer.
func (w *CausalWriter) Close() error {
	if w == nil {
		return nil
	}
	closer, ok := w.writer.(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close()
}

// NetworkStreamID builds the stream_id convention moltnet uses for
// message.accepted causal events: "network:<networkID>".
func NetworkStreamID(networkID string) string {
	return "network:" + strings.TrimSpace(networkID)
}
