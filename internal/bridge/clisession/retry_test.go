package clisession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestRunnerRotatesBusyRuntimeSessionID(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	logPath := filepath.Join(tempDir, "runtime.log")
	scriptPath := writeBusySessionRuntimeScript(t, logPath, "stale-session")
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
	runner := NewRunner(config, fakeDriver{command: scriptPath}, streamerStub{}, backoffStub{})
	if _, err := runner.store.Put("same", "stale-session"); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	err := runner.dispatch(context.Background(), Delivery{ContextKey: "same", Prompt: "hello"})
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}

	logText := readFile(t, logPath)
	if strings.Count(logText, "ATTEMPT") != 2 {
		t.Fatalf("expected stale attempt plus fresh retry, got:\n%s", logText)
	}
	if !strings.Contains(logText, "ATTEMPT stale-session true") {
		t.Fatalf("missing stale-session attempt:\n%s", logText)
	}
	if !strings.Contains(logText, "ATTEMPT ") || !strings.Contains(logText, " false") {
		t.Fatalf("missing fresh non-existing-session retry:\n%s", logText)
	}

	record, ok, err := runner.store.Get("same")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !ok || record.RuntimeSessionID == "" || record.RuntimeSessionID == "stale-session" {
		t.Fatalf("session was not rotated: ok=%v record=%#v", ok, record)
	}
}

func writeBusySessionRuntimeScript(t *testing.T, logPath string, busySessionID string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "runtime")
	script := "#!/bin/sh\n" +
		"printf 'ATTEMPT %s %s\\n' \"$2\" \"$3\" >>" + shellEscapeForTest(logPath) + "\n" +
		"if [ \"$2\" = " + shellEscapeForTest(busySessionID) + " ]; then\n" +
		"  printf 'Error: Session ID %s already in use\\n' \"$2\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"cat >>" + shellEscapeForTest(logPath) + "\n" +
		"printf 'session_id=%s\\n' \"$2\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}
