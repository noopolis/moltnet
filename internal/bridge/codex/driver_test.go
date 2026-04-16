package codex

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/noopolis/moltnet/internal/bridge/clisession"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestDriverBuildsInitialAndResumeCommands(t *testing.T) {
	t.Parallel()

	workspace := "/tmp/codex-workspace"
	config := bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{
			Command:       "/usr/local/bin/codex",
			HomePath:      "/tmp/home",
			WorkspacePath: workspace,
		},
	}
	driver := Driver{}

	initial, err := driver.BuildCommand(config, clisession.Delivery{Prompt: "hello"})
	if err != nil {
		t.Fatalf("BuildCommand() initial error = %v", err)
	}
	expectedInitial := []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--output-last-message",
		filepath.Join(workspace, ".moltnet", "codex-last-message.txt"),
		"-",
	}
	if !reflect.DeepEqual(initial.Args, expectedInitial) {
		t.Fatalf("unexpected initial args %#v", initial.Args)
	}
	if initial.Command != "/usr/local/bin/codex" || initial.Stdin != "hello" {
		t.Fatalf("unexpected initial command %#v", initial)
	}
	if len(initial.Env) != 1 || initial.Env[0] != "CODEX_HOME=/tmp/home/.codex" {
		t.Fatalf("unexpected env %#v", initial.Env)
	}

	resume, err := driver.BuildCommand(config, clisession.Delivery{
		Prompt:           "again",
		ExistingSession:  true,
		RuntimeSessionID: "session-1",
	})
	if err != nil {
		t.Fatalf("BuildCommand() resume error = %v", err)
	}
	expectedResume := []string{
		"exec",
		"resume",
		"--json",
		"--skip-git-repo-check",
		"--output-last-message",
		filepath.Join(workspace, ".moltnet", "codex-last-message.txt"),
		"session-1",
		"-",
	}
	if !reflect.DeepEqual(resume.Args, expectedResume) {
		t.Fatalf("unexpected resume args %#v", resume.Args)
	}
}

func TestDriverExtractRuntimeSessionID(t *testing.T) {
	t.Parallel()

	driver := Driver{}
	result := clisession.CommandResult{
		Stdout: "{\"type\":\"turn.start\"}\n{\"session_id\":\"codex-session-1\"}\n",
	}
	if got := driver.ExtractRuntimeSessionID(result); got != "codex-session-1" {
		t.Fatalf("unexpected session id %q", got)
	}

	nested := clisession.CommandResult{
		Stdout: "{\"event\":{\"type\":\"session.created\",\"id\":\"nested-session\"}}\n",
	}
	if got := driver.ExtractRuntimeSessionID(nested); got != "nested-session" {
		t.Fatalf("unexpected nested session id %q", got)
	}
}
