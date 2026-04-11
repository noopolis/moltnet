package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var processStateMu sync.Mutex

func TestRun(t *testing.T) {
	t.Parallel()

	if err := run(context.Background(), nil); err == nil {
		t.Fatal("expected missing args error")
	}

	if err := run(context.Background(), []string{"missing.json"}); err == nil {
		t.Fatal("expected missing file error")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.json")
	if err := os.WriteFile(path, []byte(`{
  "version":"moltnet.bridge.v1",
  "agent":{"id":"researcher"},
  "moltnet":{"base_url":"http://127.0.0.1:8787","network_id":"local"},
  "runtime":{"kind":"openclaw","gateway_url":"ws://127.0.0.1:18789"}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, []string{path}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunWithSignals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.json")
	if err := os.WriteFile(path, []byte(`{
  "version":"moltnet.bridge.v1",
  "agent":{"id":"researcher"},
  "moltnet":{"base_url":"http://127.0.0.1:8787","network_id":"local"},
  "runtime":{"kind":"openclaw","gateway_url":"ws://127.0.0.1:18789"}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runWithSignals([]string{path}, func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	})
	if err != nil {
		t.Fatalf("runWithSignals() error = %v", err)
	}
}

func TestDefaultSignalContext(t *testing.T) {
	t.Parallel()

	ctx, stop := defaultSignalContext()
	stop()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected canceled signal context")
	}
}

func TestRunVersion(t *testing.T) {
	if got := captureStdout(t, func() {
		if err := run(context.Background(), []string{"version"}); err != nil {
			t.Fatalf("run(version) error = %v", err)
		}
	}); got != version+"\n" {
		t.Fatalf("unexpected version output %q", got)
	}
}

func TestMainVersion(t *testing.T) {
	if got := captureMainOutput(t, []string{"moltnet-bridge", "version"}, func() {
		main()
	}); got != version+"\n" {
		t.Fatalf("unexpected version output %q", got)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	processStateMu.Lock()
	defer processStateMu.Unlock()

	return captureStdoutLocked(t, fn)
}

func captureMainOutput(t *testing.T, args []string, fn func()) string {
	t.Helper()

	processStateMu.Lock()
	defer processStateMu.Unlock()

	previousArgs := os.Args
	defer func() { os.Args = previousArgs }()
	os.Args = append([]string(nil), args...)

	return captureStdoutLocked(t, fn)
}

func captureStdoutLocked(t *testing.T, fn func()) string {
	t.Helper()

	current := stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer reader.Close()

	stdout = writer
	defer func() {
		stdout = current
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	return buffer.String()
}
