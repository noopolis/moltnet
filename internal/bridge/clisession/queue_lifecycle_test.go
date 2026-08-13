package clisession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type reconnectingStreamer struct {
	mu     sync.Mutex
	cycles [][]protocol.Event
	errs   []error
	before []func()
	calls  int
}

func (s *reconnectingStreamer) StreamEventsReady(
	_ context.Context,
	_ bridgeconfig.Config,
	onReady func(),
	handle func(protocol.Event) error,
) error {
	s.mu.Lock()
	cycle := s.calls
	s.calls++
	events := []protocol.Event{}
	if cycle < len(s.cycles) {
		events = append([]protocol.Event(nil), s.cycles[cycle]...)
	}

	var cycleErr error
	if cycle < len(s.errs) {
		cycleErr = s.errs[cycle]
	}

	beforeFn := func() {}
	if cycle < len(s.before) {
		beforeFn = s.before[cycle]
	}
	s.mu.Unlock()

	if onReady != nil {
		onReady()
	}
	if beforeFn != nil {
		beforeFn()
	}

	for _, event := range events {
		if err := handle(event); err != nil {
			return err
		}
	}
	return cycleErr
}

func TestRunnerReportsQueuedAndTurnLifecycleAcrossReconnect(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	logPath := filepath.Join(tempDir, "runtime.log")
	scriptPath := writeSlowPromptRuntimeScriptWithDelay(t, logPath, "0.25")
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

	started := make(chan struct{}, 1)
	var observedMu sync.Mutex
	observed := make([]struct {
		event   RunnerEventType
		payload RunnerEvent
	}, 0, 4)
	record := func(event RunnerEventType, payload RunnerEvent) {
		observedMu.Lock()
		observed = append(observed, struct {
			event   RunnerEventType
			payload RunnerEvent
		}{event: event, payload: payload})
		observedMu.Unlock()

		if event == RunnerEventTurnStarted {
			select {
			case started <- struct{}{}:
			default:
			}
		}
	}

	streamer := &reconnectingStreamer{
		cycles: [][]protocol.Event{
			{messageEvent("evt_1", "msg_1", "first <@molt://local_lab/agents/claude_bot>")},
			{
				messageEvent("evt_2", "msg_2", "second <@molt://local_lab/agents/claude_bot>"),
				messageEvent("evt_3", "msg_3", "third <@molt://local_lab/agents/claude_bot>"),
			},
		},
		errs: []error{
			errors.New("simulated stream reconnect"),
			nil,
		},
		before: []func(){
			nil,
			func() {
				select {
				case <-started:
				case <-time.After(2 * time.Second):
					t.Fatalf("timed out waiting for first turn start event")
				}
			},
		},
	}

	runner := NewRunner(config, fakeDriver{command: scriptPath}, streamer, backoffStub{})
	runner.SetObserver(record)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	observedMu.Lock()
	wakeQueuedCount := 0
	startedCount := 0
	completedCount := 0
	failedCount := 0
	wakeQueuedMessageIDs := map[string]struct{}{}
	for _, item := range observed {
		switch item.event {
		case RunnerEventWakeQueued:
			wakeQueuedCount++
			wakeQueuedMessageIDs[item.payload.MessageID] = struct{}{}
		case RunnerEventTurnStarted:
			startedCount++
		case RunnerEventTurnCompleted:
			completedCount++
		case RunnerEventTurnFailed:
			failedCount++
		}
	}
	observedMu.Unlock()

	if wakeQueuedCount != 2 {
		t.Fatalf("expected 2 wake queue events, got %d", wakeQueuedCount)
	}
	if startedCount != 2 || completedCount != 2 {
		t.Fatalf("expected 2 started/completed events, got started=%d completed=%d", startedCount, completedCount)
	}
	if failedCount != 0 {
		t.Fatalf("expected no failed events, got %d", failedCount)
	}
	if _, ok := wakeQueuedMessageIDs["msg_2"]; !ok {
		t.Fatalf("expected wake-up for msg_2, got %#v", wakeQueuedMessageIDs)
	}
	if _, ok := wakeQueuedMessageIDs["msg_3"]; !ok {
		t.Fatalf("expected wake-up for msg_3, got %#v", wakeQueuedMessageIDs)
	}

	logText := readFile(t, logPath)
	if strings.Count(logText, "START") != 2 {
		t.Fatalf("expected two runtime invocations, got:\n%s", logText)
	}
	if count := strings.Count(logText, "Queued messages: 2"); count != 1 {
		t.Fatalf("expected one queued batch, count=%d log=\n%s", count, logText)
	}
	for _, messageID := range []string{"msg_1", "msg_2", "msg_3"} {
		if count := strings.Count(logText, "Message ID: "+messageID); count != 1 {
			t.Fatalf("expected message id %s delivered once, count=%d log=\n%s", messageID, count, logText)
		}
	}
}

func TestRunnerReportsTurnFailureLifecycle(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	logPath := filepath.Join(tempDir, "runtime.log")
	scriptPath := writeFailingRuntimeScript(t, logPath)
	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "claude_bot", Name: "Claude Bot"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet", NetworkID: "local_lab"},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:             bridgeconfig.RuntimeClaudeCode,
			Command:          scriptPath,
			WorkspacePath:    workspace,
			SessionStorePath: filepath.Join(workspace, ".moltnet", "sessions.json"),
		},
	}

	var observedMu sync.Mutex
	observed := make([]struct {
		event   RunnerEventType
		payload RunnerEvent
	}, 0, 3)
	runner := NewRunner(config, fakeDriver{command: scriptPath}, streamerStub{}, backoffStub{})
	runner.SetObserver(func(event RunnerEventType, payload RunnerEvent) {
		observedMu.Lock()
		observed = append(observed, struct {
			event   RunnerEventType
			payload RunnerEvent
		}{event: event, payload: payload})
		observedMu.Unlock()
	})

	err := runner.dispatch(context.Background(), Delivery{ContextKey: "same", MessageID: "fail", Prompt: "hello"})
	if err == nil {
		t.Fatalf("expected command failure")
	}

	observedMu.Lock()
	startedCount := 0
	completedCount := 0
	failedCount := 0
	for _, item := range observed {
		switch item.event {
		case RunnerEventTurnStarted:
			startedCount++
		case RunnerEventTurnCompleted:
			completedCount++
		case RunnerEventTurnFailed:
			failedCount++
		}
	}
	observedMu.Unlock()

	if startedCount != 1 || completedCount != 0 || failedCount != 1 {
		t.Fatalf("expected started=1 completed=0 failed=1, got started=%d completed=%d failed=%d", startedCount, completedCount, failedCount)
	}
}

func writeSlowPromptRuntimeScriptWithDelay(t *testing.T, logPath string, delay string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "runtime")
	script := "#!/bin/sh\n" +
		"printf 'START %s %s\\n' \"$2\" \"$3\" >>" + shellEscapeForTest(logPath) + "\n" +
		"cat >>" + shellEscapeForTest(logPath) + "\n" +
		"printf '\\nEND %s\\n' \"$2\" >>" + shellEscapeForTest(logPath) + "\n" +
		"sleep " + delay + "\n" +
		"printf 'session_id=%s\\n' \"$2\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

func writeFailingRuntimeScript(t *testing.T, logPath string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "runtime")
	script := "#!/bin/sh\n" +
		"printf 'FAIL\\n' >>" + shellEscapeForTest(logPath) + "\n" +
		"exit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}
