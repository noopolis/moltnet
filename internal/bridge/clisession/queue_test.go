package clisession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type pacedStreamer struct {
	events      []protocol.Event
	afterFirst  func()
	afterEvents func()
}

func (s pacedStreamer) StreamEventsReady(
	_ context.Context,
	_ bridgeconfig.Config,
	onReady func(),
	handle func(protocol.Event) error,
) error {
	if onReady != nil {
		onReady()
	}
	for index, event := range s.events {
		if err := handle(event); err != nil {
			return err
		}
		if index == 0 && s.afterFirst != nil {
			s.afterFirst()
		}
	}
	if s.afterEvents != nil {
		s.afterEvents()
	}
	return nil
}

func TestRunnerQueuesMessagesWhileRuntimeIsActive(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	logPath := filepath.Join(tempDir, "runtime.log")
	runtimeStartedPath := filepath.Join(tempDir, "runtime-started")
	runtimeReleasePath := filepath.Join(tempDir, "runtime-release")

	// Use a two-way named-pipe handshake instead of polling runtime.log.
	// Child-process startup can exceed a short wall-clock deadline under
	// parallel package load. The start signal proves the first command is
	// executing, and the release signal keeps it active until both later
	// events have been queued.
	// If the runtime command fails to start, the blocking pipe operations hang
	// until the go test timeout; the resulting goroutine dump is the intended
	// diagnostic. This is a deliberate trade for determinism.
	makeNamedPipe(t, runtimeStartedPath)
	makeNamedPipe(t, runtimeReleasePath)

	scriptPath := writeBlockedPromptRuntimeScript(
		t,
		logPath,
		runtimeStartedPath,
		runtimeReleasePath,
	)
	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "claude_bot", Name: "Claude Bot"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet", NetworkID: "local_lab"},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:             bridgeconfig.RuntimeClaudeCode,
			Command:          scriptPath,
			WorkspacePath:    workspace,
			SessionStorePath: filepath.Join(workspace, ".moltnet", "sessions.json"),
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Wake: bridgeconfig.WakeMentions},
		},
	}

	events := []protocol.Event{
		messageEvent("evt_1", "msg_1", "first <@molt://local_lab/agents/claude_bot>"),
		messageEvent("evt_2", "msg_2", "second <@molt://local_lab/agents/claude_bot>"),
		messageEvent("evt_3", "msg_3", "third <@molt://local_lab/agents/claude_bot>"),
	}
	streamer := pacedStreamer{
		events: events,
		afterFirst: func() {
			if got := readNamedPipe(t, runtimeStartedPath); got != "START\n" {
				t.Fatalf("runtime start signal = %q, want %q", got, "START\n")
			}
		},
		afterEvents: func() {
			writeNamedPipe(t, runtimeReleasePath, "release\n")
		},
	}

	err := Run(context.Background(), config, fakeDriver{command: scriptPath}, streamer, backoffStub{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	logText := readFile(t, logPath)
	if strings.Count(logText, "START") != 2 {
		t.Fatalf("expected first wake plus one queued batch, got:\n%s", logText)
	}
	for _, want := range []string{
		"Message:\nfirst <@molt://local_lab/agents/claude_bot>",
		"Queued messages: 2",
		"--- Message 1/2 ---",
		"Message ID: msg_2",
		"Message:\nsecond <@molt://local_lab/agents/claude_bot>",
		"--- Message 2/2 ---",
		"Message ID: msg_3",
		"Message:\nthird <@molt://local_lab/agents/claude_bot>",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("queued runtime log missing %q:\n%s", want, logText)
		}
	}
}

func messageEvent(eventID string, messageID string, text string) protocol.Event {
	return protocol.Event{
		ID:        eventID,
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local_lab",
		Message: &protocol.Message{
			ID:        messageID,
			NetworkID: "local_lab",
			Target: protocol.Target{
				Kind:   protocol.TargetKindRoom,
				RoomID: "research",
			},
			From:     protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Mentions: []string{protocol.AgentFQID("local_lab", "claude_bot")},
			Parts:    []protocol.Part{{Kind: protocol.PartKindText, Text: text}},
		},
	}
}

func writeBlockedPromptRuntimeScript(
	t *testing.T,
	logPath string,
	runtimeStartedPath string,
	runtimeReleasePath string,
) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "runtime")
	script := "#!/bin/sh\n" +
		"printf 'START %s %s\\n' \"$2\" \"$3\" >>" + shellEscapeForTest(logPath) + "\n" +
		"if [ \"$3\" = \"false\" ]; then\n" +
		"  printf 'START\\n' >" + shellEscapeForTest(runtimeStartedPath) + "\n" +
		"  read -r _ <" + shellEscapeForTest(runtimeReleasePath) + "\n" +
		"fi\n" +
		"cat >>" + shellEscapeForTest(logPath) + "\n" +
		"printf '\\nEND %s\\n' \"$2\" >>" + shellEscapeForTest(logPath) + "\n" +
		"printf 'session_id=%s\\n' \"$2\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(bytes)
}

func makeNamedPipe(t *testing.T, path string) {
	t.Helper()

	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("make named pipe %s: %v", path, err)
	}
}

func readNamedPipe(t *testing.T, path string) string {
	t.Helper()

	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read named pipe %s: %v", path, err)
	}
	return string(bytes)
}

func writeNamedPipe(t *testing.T, path string, text string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open named pipe %s for writing: %v", path, err)
	}
	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()
		t.Fatalf("write named pipe %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close named pipe %s: %v", path, err)
	}
}
