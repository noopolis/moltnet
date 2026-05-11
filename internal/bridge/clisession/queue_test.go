package clisession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type pacedStreamer struct {
	events     []protocol.Event
	afterFirst func()
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
	scriptPath := writeSlowPromptRuntimeScript(t, logPath)
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
			{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto},
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
			waitForFileContains(t, logPath, "START")
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

func writeSlowPromptRuntimeScript(t *testing.T, logPath string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "runtime")
	script := "#!/bin/sh\n" +
		"printf 'START %s %s\\n' \"$2\" \"$3\" >>" + shellEscapeForTest(logPath) + "\n" +
		"cat >>" + shellEscapeForTest(logPath) + "\n" +
		"printf '\\nEND %s\\n' \"$2\" >>" + shellEscapeForTest(logPath) + "\n" +
		"sleep 0.15\n" +
		"printf 'session_id=%s\\n' \"$2\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

func waitForFileContains(t *testing.T, path string, text string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(readFileIfExists(path), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %s:\n%s", text, path, readFileIfExists(path))
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(bytes)
}

func readFileIfExists(path string) string {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(bytes)
}
