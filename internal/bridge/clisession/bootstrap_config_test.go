package clisession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestRunnerWritesWorkspaceClientConfigBeforeBootstrap(t *testing.T) {
	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	tokenPath := filepath.Join(tempDir, "alpha.token")
	if err := os.WriteFile(tokenPath, []byte("magt_v1_alpha\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	logPath := filepath.Join(tempDir, "runtime.log")
	scriptPath := filepath.Join(tempDir, "runtime")
	script := "#!/bin/sh\n" +
		"cat \"$MOLTNET_CLIENT_CONFIG\" >>" + shellEscapeForTest(logPath) + "\n" +
		"cat >>" + shellEscapeForTest(logPath) + "\n" +
		"printf '\\nDONE\\n' >>" + shellEscapeForTest(logPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "alpha", Name: "Alpha"},
		Moltnet: bridgeconfig.MoltnetConfig{
			AuthMode:  bridgeconfig.AuthModeOpen,
			BaseURL:   "http://moltnet",
			NetworkID: "local",
			TokenPath: tokenPath,
		},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:          bridgeconfig.RuntimeCodex,
			Command:       scriptPath,
			WorkspacePath: workspace,
		},
		Rooms: []bridgeconfig.RoomBinding{{ID: "agora", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto}},
	}

	err := Run(context.Background(), config, fakeDriver{command: scriptPath}, streamerStub{}, backoffStub{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, `"token": "magt_v1_alpha"`) {
		t.Fatalf("runtime did not see generated config before bootstrap:\n%s", logText)
	}
	if !strings.Contains(logText, "Moltnet conversation attached") {
		t.Fatalf("bootstrap prompt missing from runtime log:\n%s", logText)
	}
}
