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

type streamerStub struct {
	events []protocol.Event
}

func (s streamerStub) StreamEvents(_ context.Context, _ bridgeconfig.Config, handle func(protocol.Event) error) error {
	for _, event := range s.events {
		if err := handle(event); err != nil {
			return err
		}
	}
	return nil
}

type backoffStub struct{}

func (backoffStub) Delay(int) time.Duration {
	return time.Millisecond
}

type fakeDriver struct {
	command string
}

func (d fakeDriver) Name() string {
	return "fake-cli"
}

func (d fakeDriver) DefaultCommand() string {
	return d.command
}

func (d fakeDriver) UsesSessionIDForFirstDelivery() bool {
	return true
}

func (d fakeDriver) BuildCommand(_ bridgeconfig.Config, delivery Delivery) (CommandSpec, error) {
	return CommandSpec{
		Command: d.command,
		Args: []string{
			delivery.ContextKey,
			delivery.RuntimeSessionID,
			boolText(delivery.ExistingSession),
		},
		Stdin: delivery.Prompt,
	}, nil
}

func (d fakeDriver) ExtractRuntimeSessionID(result CommandResult) string {
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.HasPrefix(line, "session_id=") {
			return strings.TrimPrefix(line, "session_id=")
		}
	}
	return ""
}

func TestRunnerDeliversBootstrapAndMessageThroughCLI(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	logPath := filepath.Join(tempDir, "runtime.log")
	scriptPath := writeFakeRuntimeScript(t, logPath, "parsed-session")

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "codex_bot", Name: "Codex Bot"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://moltnet",
			NetworkID: "local_lab",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:             bridgeconfig.RuntimeCodex,
			Command:          scriptPath,
			WorkspacePath:    workspace,
			SessionStorePath: filepath.Join(workspace, ".moltnet", "sessions.json"),
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto},
		},
	}

	event := protocol.Event{
		ID:        "evt_1",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local_lab",
		Message: &protocol.Message{
			ID:        "msg_1",
			NetworkID: "local_lab",
			Target: protocol.Target{
				Kind:   protocol.TargetKindRoom,
				RoomID: "research",
			},
			From: protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Mentions: []string{
				protocol.AgentFQID("local_lab", "codex_bot"),
			},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello <@molt://local_lab/agents/codex_bot>"}},
		},
	}

	err := Run(
		context.Background(),
		config,
		fakeDriver{command: scriptPath},
		streamerStub{events: []protocol.Event{event}},
		backoffStub{},
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(logBytes)
	if strings.Count(logText, "ARGV=") != 2 {
		t.Fatalf("expected bootstrap and message invocations, got:\n%s", logText)
	}
	for _, want := range []string{
		"PWD=" + workspace,
		"MOLTNET_CLIENT_CONFIG=" + filepath.Join(workspace, ".moltnet", "config.json"),
		"ARGV=agent:codex_bot:local_lab:room:research|",
		"Channel: moltnet",
		"Chat ID: local_lab:room:research",
		"Moltnet conversation attached. Use the `moltnet` skill in this conversation.",
		"From: local_lab/agent/writer",
		"Mentions: molt://local_lab/agents/codex_bot",
		"Message:\nhello <@molt://local_lab/agents/codex_bot>",
		"ARGV=agent:codex_bot:local_lab:room:research|parsed-session|true",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log missing %q:\n%s", want, logText)
		}
	}

	storeBytes, err := os.ReadFile(filepath.Join(workspace, ".moltnet", "sessions.json"))
	if err != nil {
		t.Fatalf("read session store: %v", err)
	}
	if !strings.Contains(string(storeBytes), `"runtime_session_id": "parsed-session"`) {
		t.Fatalf("unexpected session store %s", storeBytes)
	}
}

func TestRunnerSerializesSameSessionDeliveries(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	logPath := filepath.Join(tempDir, "runtime.log")
	scriptPath := writeSlowRuntimeScript(t, logPath)

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "bot"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet", NetworkID: "local_lab"},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:             bridgeconfig.RuntimeClaudeCode,
			Command:          scriptPath,
			WorkspacePath:    workspace,
			SessionStorePath: filepath.Join(workspace, ".moltnet", "sessions.json"),
		},
	}
	runner := NewRunner(config, fakeDriver{command: scriptPath}, streamerStub{}, backoffStub{})

	errCh := make(chan error, 2)
	go func() {
		errCh <- runner.dispatch(context.Background(), Delivery{ContextKey: "same", Prompt: "one"})
	}()
	go func() {
		errCh <- runner.dispatch(context.Background(), Delivery{ContextKey: "same", Prompt: "two"})
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("dispatch() error = %v", err)
		}
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(logBytes)
	firstStart := strings.Index(logText, "START")
	firstEnd := strings.Index(logText, "END")
	secondStart := strings.LastIndex(logText, "START")
	if firstStart == -1 || firstEnd == -1 || secondStart == -1 || firstStart == secondStart {
		t.Fatalf("unexpected log:\n%s", logText)
	}
	if secondStart < firstEnd {
		t.Fatalf("expected same-session deliveries to serialize, got:\n%s", logText)
	}
}

func writeFakeRuntimeScript(t *testing.T, logPath string, sessionID string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "runtime")
	script := "#!/bin/sh\n" +
		"printf 'ARGV=%s|%s|%s\\n' \"$1\" \"$2\" \"$3\" >>" + shellEscapeForTest(logPath) + "\n" +
		"printf 'PWD=%s\\n' \"$PWD\" >>" + shellEscapeForTest(logPath) + "\n" +
		"printf 'MOLTNET_CLIENT_CONFIG=%s\\n' \"$MOLTNET_CLIENT_CONFIG\" >>" + shellEscapeForTest(logPath) + "\n" +
		"cat >>" + shellEscapeForTest(logPath) + "\n" +
		"printf '\\n---\\n' >>" + shellEscapeForTest(logPath) + "\n" +
		"printf 'session_id=%s\\n' " + shellEscapeForTest(sessionID) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

func writeSlowRuntimeScript(t *testing.T, logPath string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "runtime")
	script := "#!/bin/sh\n" +
		"printf 'START %s\\n' \"$2\" >>" + shellEscapeForTest(logPath) + "\n" +
		"sleep 0.05\n" +
		"printf 'END %s\\n' \"$2\" >>" + shellEscapeForTest(logPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

func shellEscapeForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
