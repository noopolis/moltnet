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
	directory := t.TempDir()
	writeNodeConfig(t, filepath.Join(directory, "MoltnetNode"), `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
attachments: []
`)
	t.Chdir(directory)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := run(ctx, nil); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunWithSignals(t *testing.T) {
	directory := t.TempDir()
	writeNodeConfig(t, filepath.Join(directory, "MoltnetNode"), `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
attachments: []
`)
	t.Chdir(directory)

	err := runWithSignals(nil, func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 50*time.Millisecond)
	})
	if err != nil {
		t.Fatalf("runWithSignals() error = %v", err)
	}
}

func TestRunErrorsWhenConfigMissing(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	if err := run(context.Background(), nil); err == nil {
		t.Fatal("expected missing config error")
	}
}

func TestRunUsesExplicitArgPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-node.yaml")
	writeNodeConfig(t, path, `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
attachments: []
`)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := run(ctx, []string{path}); err != nil {
		t.Fatalf("run() explicit path error = %v", err)
	}
}

func TestRunErrorsOnTooManyArgs(t *testing.T) {
	if err := run(context.Background(), []string{"one", "two"}); err == nil {
		t.Fatal("expected invalid args error")
	}
}

func TestRunUsesEnvConfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-node.yaml")
	writeNodeConfig(t, path, `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
attachments: []
`)
	t.Setenv("MOLTNET_NODE_CONFIG", path)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := run(ctx, nil); err != nil {
		t.Fatalf("run() env config error = %v", err)
	}
}

func TestRunErrorsOnInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid-node.yaml")
	if err := os.WriteFile(path, []byte("version: ["), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	if err := run(context.Background(), []string{path}); err == nil {
		t.Fatal("expected invalid config error")
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
	if got := captureMainOutput(t, []string{"moltnet-node", "version"}, func() {
		main()
	}); got != version+"\n" {
		t.Fatalf("unexpected version output %q", got)
	}
}

func TestDefaultSignalContext(t *testing.T) {
	ctx, stop := defaultSignalContext()
	stop()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected canceled signal context")
	}
}

func writeNodeConfig(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config %q: %v", path, err)
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
