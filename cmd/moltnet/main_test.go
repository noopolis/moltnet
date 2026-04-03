package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var processStateMu sync.Mutex

func TestRunStartsServerByDefault(t *testing.T) {
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MOLTNET_NETWORK_ID", "local")
	t.Setenv("MOLTNET_NETWORK_NAME", "Local")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := run(ctx, nil, "test"); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunStartsServerExplicitly(t *testing.T) {
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MOLTNET_NETWORK_ID", "local")
	t.Setenv("MOLTNET_NETWORK_NAME", "Local")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := run(ctx, []string{"start"}, "test"); err != nil {
		t.Fatalf("run() explicit start error = %v", err)
	}
}

func TestRunStartsServerAlias(t *testing.T) {
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MOLTNET_NETWORK_ID", "local")
	t.Setenv("MOLTNET_NETWORK_NAME", "Local")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := run(ctx, []string{"server"}, "test"); err != nil {
		t.Fatalf("run() server alias error = %v", err)
	}
}

func TestRunNodeCommand(t *testing.T) {
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

	if err := run(ctx, []string{"node", "start"}, "test"); err != nil {
		t.Fatalf("run() node start error = %v", err)
	}
}

func TestRunNodeCommandWithoutSubcommand(t *testing.T) {
	directory := t.TempDir()
	writeNodeConfig(t, filepath.Join(directory, "MoltnetNode"), `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
attachments: []
`)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := run(ctx, []string{"node", directory + "/MoltnetNode"}, "test"); err != nil {
		t.Fatalf("run() node direct path error = %v", err)
	}
}

func TestRunNodeUsesEnvPath(t *testing.T) {
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

	if err := runNode(ctx, nil); err != nil {
		t.Fatalf("runNode() env path error = %v", err)
	}
}

func TestRunNodeHelpCommand(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"node", "help"}, "test"); err != nil {
			t.Fatalf("run() node help error = %v", err)
		}
	})

	if !strings.Contains(output, "moltnet node start") {
		t.Fatalf("expected node help output, got %q", output)
	}
}

func TestRunNodeErrorsWhenConfigMissing(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	if err := runNode(context.Background(), nil); err == nil {
		t.Fatal("expected missing config error")
	}
}

func TestRunNodeErrorsOnTooManyArgs(t *testing.T) {
	if err := runNode(context.Background(), []string{"one", "two"}); err == nil {
		t.Fatal("expected invalid node args error")
	}
}

func TestRunAttachmentCommand(t *testing.T) {
	path := writeAttachmentConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, []string{"attachment", "run", path}, "test"); err != nil {
		t.Fatalf("run() attachment run error = %v", err)
	}
}

func TestRunAttachmentCommandWithoutSubcommand(t *testing.T) {
	path := writeAttachmentConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, []string{"attachment", path}, "test"); err != nil {
		t.Fatalf("run() attachment direct path error = %v", err)
	}
}

func TestRunAttachmentHelpCommand(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"attachment", "help"}, "test"); err != nil {
			t.Fatalf("run() attachment help error = %v", err)
		}
	})

	if !strings.Contains(output, "moltnet attachment run") {
		t.Fatalf("expected attachment help output, got %q", output)
	}
}

func TestRunAttachmentCommandErrorsWithoutPath(t *testing.T) {
	err := run(context.Background(), []string{"attachment"}, "test")
	if err == nil {
		t.Fatal("expected missing attachment path error")
	}
	if !strings.Contains(err.Error(), "attachment runner config path required") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRunVersionCommand(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"version"}, "1.2.3"); err != nil {
			t.Fatalf("run() version error = %v", err)
		}
	})

	if strings.TrimSpace(output) != "1.2.3" {
		t.Fatalf("unexpected version output %q", output)
	}
}

func TestRunHelpCommand(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"help"}, "test"); err != nil {
			t.Fatalf("run() help error = %v", err)
		}
	})

	if !strings.Contains(output, "moltnet start") {
		t.Fatalf("expected help output, got %q", output)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	err := run(context.Background(), []string{"wat"}, "test")
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "wat"`) {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRunAttachmentRejectsTooManyArgs(t *testing.T) {
	err := runAttachment(context.Background(), []string{"one", "two"})
	if err == nil {
		t.Fatal("expected invalid attachment args error")
	}
}

func TestRunAttachmentErrorsWhenFileMissing(t *testing.T) {
	if err := runAttachment(context.Background(), []string{"missing.json"}); err == nil {
		t.Fatal("expected missing attachment file error")
	}
}

func TestRunServerErrorsOnInvalidConfigPath(t *testing.T) {
	t.Setenv("MOLTNET_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))

	if err := runServer(context.Background(), "test"); err == nil {
		t.Fatal("expected runServer error for invalid config path")
	}
}

func TestRunCLIWithSignals(t *testing.T) {
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MOLTNET_NETWORK_ID", "local")
	t.Setenv("MOLTNET_NETWORK_NAME", "Local")

	err := runCLI([]string{"start"}, "test", func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 50*time.Millisecond)
	})
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
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

func TestRunMainUsesVersion(t *testing.T) {
	output := captureStdout(t, func() {
		if err := runMain([]string{"version"}); err != nil {
			t.Fatalf("runMain() error = %v", err)
		}
	})

	if strings.TrimSpace(output) != version {
		t.Fatalf("unexpected version output %q", output)
	}
}

func TestMainVersion(t *testing.T) {
	output := captureMainOutput(t, []string{"moltnet", "version"}, func() {
		main()
	})
	if strings.TrimSpace(output) != version {
		t.Fatalf("unexpected main() version output %q", output)
	}
}

func writeNodeConfig(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config %q: %v", path, err)
	}
}

func writeAttachmentConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "attachment.json")
	contents := `{
  "version":"moltnet.bridge.v1",
  "agent":{"id":"researcher"},
  "moltnet":{"base_url":"http://127.0.0.1:8787","network_id":"local"},
  "runtime":{"kind":"openclaw","control_url":"http://127.0.0.1:9100/team/message"}
}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write attachment config: %v", err)
	}

	return path
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
