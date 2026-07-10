package observability

import (
	"bufio"
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func newTestCausalEvent(runID, streamID string, cause []string) protocol.CausalEvent {
	if cause == nil {
		cause = []string{}
	}
	return protocol.CausalEvent{
		Version: protocol.CausalEventVersion,
		RunID:   runID,
		EventID: "moltnet:msg_" + streamID,
		Emitter: protocol.CausalEmitter{
			System:   protocol.CausalSystemMoltnet,
			StreamID: streamID,
		},
		Type:          protocol.EventTypeMessageAccepted,
		PrincipalID:   "token:writer",
		RecordedAt:    time.Now().UTC(),
		CauseEventIDs: cause,
		Payload:       json.RawMessage(`{"message_id":"msg_1"}`),
	}
}

func TestCausalWriterAssignsContiguousSeqPerStream(t *testing.T) {
	var buf bytes.Buffer
	writer := NewCausalWriter(&buf)

	first, err := writer.Append(newTestCausalEvent("run_1", "network:local", nil))
	if err != nil {
		t.Fatalf("first Append() error = %v", err)
	}
	second, err := writer.Append(newTestCausalEvent("run_1", "network:local", nil))
	if err != nil {
		t.Fatalf("second Append() error = %v", err)
	}
	if first.Emitter.Seq != 1 || second.Emitter.Seq != 2 {
		t.Fatalf("expected contiguous seq 1,2 got %d,%d", first.Emitter.Seq, second.Emitter.Seq)
	}

	// A distinct stream within the same run starts its own seq at 1.
	otherStream, err := writer.Append(newTestCausalEvent("run_1", "network:other", nil))
	if err != nil {
		t.Fatalf("other-stream Append() error = %v", err)
	}
	if otherStream.Emitter.Seq != 1 {
		t.Fatalf("expected seq 1 for a new stream, got %d", otherStream.Emitter.Seq)
	}

	// A distinct run within the same stream_id also starts its own seq at 1.
	otherRun, err := writer.Append(newTestCausalEvent("run_2", "network:local", nil))
	if err != nil {
		t.Fatalf("other-run Append() error = %v", err)
	}
	if otherRun.Emitter.Seq != 1 {
		t.Fatalf("expected seq 1 for a new run, got %d", otherRun.Emitter.Seq)
	}

	scanner := bufio.NewScanner(&buf)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != 4 {
		t.Fatalf("expected 4 JSONL lines, got %d: %v", len(lines), lines)
	}
	for _, line := range lines {
		var decoded protocol.CausalEvent
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("unmarshal line %q: %v", line, err)
		}
		if err := decoded.Validate(); err != nil {
			t.Fatalf("decoded line failed Validate(): %v (line=%s)", err, line)
		}
	}
}

func TestCausalWriterRejectsInvalidEventWithoutAdvancingSeq(t *testing.T) {
	var buf bytes.Buffer
	writer := NewCausalWriter(&buf)

	invalid := newTestCausalEvent("run_1", "network:local", nil)
	invalid.Type = "" // required field left blank makes Validate() fail

	if _, err := writer.Append(invalid); err == nil {
		t.Fatal("expected Append() to fail for an invalid event")
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no bytes written for a rejected event, got %q", buf.String())
	}

	valid, err := writer.Append(newTestCausalEvent("run_1", "network:local", nil))
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if valid.Emitter.Seq != 1 {
		t.Fatalf("expected the rejected append to not consume seq 1, got %d", valid.Emitter.Seq)
	}
}

func TestCausalWriterAppendIsSerializedUnderConcurrency(t *testing.T) {
	var buf bytes.Buffer
	writer := NewCausalWriter(&buf)

	const goroutines = 20
	done := make(chan int, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			event, err := writer.Append(newTestCausalEvent("run_1", "network:local", nil))
			if err != nil {
				done <- -1
				return
			}
			done <- event.Emitter.Seq
		}()
	}

	seen := make(map[int]bool, goroutines)
	for i := 0; i < goroutines; i++ {
		seq := <-done
		if seq < 1 {
			t.Fatalf("Append() returned an error result")
		}
		if seen[seq] {
			t.Fatalf("seq %d assigned more than once", seq)
		}
		seen[seq] = true
	}
	for seq := 1; seq <= goroutines; seq++ {
		if !seen[seq] {
			t.Fatalf("missing seq %d in contiguous run of %d appends", seq, goroutines)
		}
	}
}

func TestNewCausalFileWriterCreatesParentDirectoryAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "causal.jsonl")

	writer, err := NewCausalFileWriter(path)
	if err != nil {
		t.Fatalf("NewCausalFileWriter() error = %v", err)
	}
	defer writer.Close()

	if _, err := writer.Append(newTestCausalEvent("run_1", "network:local", nil)); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNetworkStreamIDConvention(t *testing.T) {
	if got := NetworkStreamID("local"); got != "network:local" {
		t.Fatalf("NetworkStreamID() = %q, want %q", got, "network:local")
	}
}
