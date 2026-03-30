package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
  "runtime":{"kind":"openclaw","control_url":"http://127.0.0.1:9100/team/message"}
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
  "runtime":{"kind":"openclaw","control_url":"http://127.0.0.1:9100/team/message"}
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
