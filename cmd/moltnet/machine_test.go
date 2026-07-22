package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/internal/machine"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

func TestMachineSelectionAndSharedRegistry(t *testing.T) {
	path := writeMachineConfig(t, `{
  "version":"moltnet.client.v1",
  "attachments":[
    {"base_url":"https://url-secret.invalid/a","network_id":"net-a","member_id":"member-a","auth":{"mode":"bearer","token":"token-secret-a"}},
    {"base_url":"https://url-secret.invalid/b","network_id":"net-b","member_id":"member-b","auth":{"mode":"bearer","token":"token-secret-b"}}
  ]
}`)
	var output bytes.Buffer
	err := runMachineWithIO(context.Background(), []string{"--config", path}, machineIO{input: io.NopCloser(strings.NewReader("")), output: &output})
	if err != errMachineStartup {
		t.Fatalf("ambiguous selection error = %v", err)
	}
	assertMachineSecretsAbsent(t, err, output.String())

	previous := runMachineSession
	defer func() { runMachineSession = previous }()
	called := false
	runMachineSession = func(_ context.Context, attachment clientconfig.AttachmentConfig, configured *client.Client, registry machine.DeliveryRegistry, executor machine.Executor, _ machineIO) error {
		called = true
		if attachment.NetworkID != "net-b" || attachment.MemberID != "member-b" || configured == nil {
			t.Fatalf("selected attachment = %+v client=%v", attachment, configured)
		}
		if registry.Size() != 0 || registryPointer(executor) != reflect.ValueOf(registry).Pointer() {
			t.Fatal("machine session and provider executor do not share one registry")
		}
		return nil
	}
	err = runMachineWithIO(context.Background(), []string{"--config", path, "--network", "net-b", "--member", "member-b"}, machineIO{input: io.NopCloser(strings.NewReader("")), output: &output})
	if err != nil || !called {
		t.Fatalf("explicit selection err=%v called=%v", err, called)
	}
}

func TestMachineEOFAndSignalCancellation(t *testing.T) {
	path := writeMachineConfig(t, machineOpenConfig())
	input := newObservedMachineReader(false)
	var output bytes.Buffer
	eofDone := make(chan error, 1)
	go func() {
		eofDone <- runMachineWithIO(context.Background(), []string{"--config", path}, machineIO{input: input, output: &output})
	}()
	waitMachineSignal(t, "EOF read entry", input.readStarted)
	waitMachineSignal(t, "EOF read exit", input.readExited)
	waitMachineSignal(t, "EOF input close", input.closed)
	if err := waitMachineResult(t, eofDone); err != nil {
		t.Fatalf("EOF run: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("EOF output = %q", output.String())
	}

	blocked := newObservedMachineReader(true)
	output.Reset()
	previous := machineStdio
	machineStdio = func() machineIO { return machineIO{input: blocked, output: &output} }
	defer func() { machineStdio = previous }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runCLI([]string{"machine", "--config", path}, "test", func() (context.Context, context.CancelFunc) { return ctx, cancel })
	}()
	waitMachineSignal(t, "blocked read entry", blocked.readStarted)
	cancel()
	waitMachineSignal(t, "blocked input close", blocked.closed)
	waitMachineSignal(t, "blocked read exit", blocked.readExited)
	if err := waitMachineResult(t, done); err != nil {
		t.Fatalf("cancelled machine: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("cancel output = %q", output.String())
	}
}

func TestMachineHelpArgumentsAndExistingDispatch(t *testing.T) {
	for _, args := range [][]string{{"machine", "help"}, {"machine", "--help"}} {
		var output bytes.Buffer
		withMachineStdout(t, &output, func() {
			if err := run(context.Background(), args, "test"); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
		})
		if !strings.Contains(output.String(), "moltnet machine --config") || strings.Contains(output.String(), "moltnet send") {
			t.Fatalf("machine help = %q", output.String())
		}
	}
	var output bytes.Buffer
	withMachineStdout(t, &output, func() {
		if err := run(context.Background(), []string{"help"}, "test"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output.String(), "moltnet machine --config") || !strings.Contains(output.String(), "moltnet send") {
		t.Fatalf("top-level usage = %q", output.String())
	}
	if err := runMachineWithIO(context.Background(), []string{"--config", "x", "extra"}, machineIO{}); err == nil || !strings.Contains(err.Error(), "positional") {
		t.Fatalf("extra arguments error = %v", err)
	}
	withMachineStdout(t, &output, func() {
		if err := runMachineWithIO(context.Background(), []string{"--unknown"}, machineIO{}); err == nil {
			t.Fatal("unknown flag accepted")
		}
	})
	if !strings.Contains(output.String(), "flag provided but not defined") {
		t.Fatalf("unknown flag output = %q", output.String())
	}
	output.Reset()
	withMachineStdout(t, &output, func() {
		if err := run(context.Background(), []string{"version"}, "unchanged-version"); err != nil {
			t.Fatalf("version dispatch: %v", err)
		}
	})
	if output.String() != "unchanged-version\n" {
		t.Fatalf("version output = %q", output.String())
	}
}

func writeMachineConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "machine.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func registryPointer(executor machine.Executor) uintptr {
	value := reflect.ValueOf(executor).Elem().FieldByName("delivery").Elem().FieldByName("registry").Elem()
	return value.Pointer()
}

func machineOpenConfig() string {
	return `{"version":"moltnet.client.v1","attachments":[{"base_url":"https://url-secret.invalid","network_id":"net","member_id":"member","auth":{"mode":"open"}}]}`
}

func assertMachineSecretsAbsent(t *testing.T, err error, output string) {
	t.Helper()
	value := output
	if err != nil {
		value += err.Error()
	}
	for _, secret := range []string{"url-secret", "token-secret", "Bearer", "header-secret", "provider-body"} {
		if strings.Contains(value, secret) {
			t.Fatalf("secret %q leaked in %q", secret, value)
		}
	}
}

func withMachineStdout(t *testing.T, writer io.Writer, fn func()) {
	t.Helper()
	previous := stdout
	stdout = writer
	defer func() { stdout = previous }()
	fn()
}

type observedMachineReader struct {
	block       bool
	readStarted chan struct{}
	readExited  chan struct{}
	closed      chan struct{}
	once        sync.Once
}

func newObservedMachineReader(block bool) *observedMachineReader {
	return &observedMachineReader{block: block, readStarted: make(chan struct{}), readExited: make(chan struct{}), closed: make(chan struct{})}
}

func (reader *observedMachineReader) Read([]byte) (int, error) {
	close(reader.readStarted)
	if reader.block {
		<-reader.closed
	}
	close(reader.readExited)
	return 0, io.EOF
}

func (reader *observedMachineReader) Close() error {
	reader.once.Do(func() { close(reader.closed) })
	return nil
}

func waitMachineSignal(t *testing.T, label string, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitMachineResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for machine return")
	}
	return nil
}
