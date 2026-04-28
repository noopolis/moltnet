package node

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	cliRuntimeIntegrationPollInterval = 20 * time.Millisecond
	cliRuntimeIntegrationWaitTimeout  = 30 * time.Second
)

func TestNodeRunsCodexAndClaudeCodeAttachmentsAgainstMoltnet(t *testing.T) {
	instance, err := app.New(app.Config{
		AllowHumanIngress: true,
		ListenAddr:        ":0",
		NetworkID:         "local_lab",
		NetworkName:       "Local Lab",
		Rooms: []app.RoomConfig{{
			ID:      "research",
			Members: []string{"codex_bot", "claude_bot"},
		}},
		Version: "test",
	})
	if err != nil {
		t.Fatalf("app.New() error = %v", err)
	}
	t.Cleanup(func() { _ = instance.Close() })

	server := httptest.NewServer(instance.Handler())
	t.Cleanup(server.Close)

	tempDir := t.TempDir()
	codexWorkspace := filepath.Join(tempDir, "codex-workspace")
	claudeWorkspace := filepath.Join(tempDir, "claude-workspace")
	for _, path := range []string{codexWorkspace, claudeWorkspace} {
		if err := os.MkdirAll(filepath.Join(path, ".moltnet"), 0o755); err != nil {
			t.Fatalf("mkdir workspace: %v", err)
		}
	}
	codexLog := filepath.Join(tempDir, "codex.log")
	claudeLog := filepath.Join(tempDir, "claude.log")

	runner, err := New(nodeconfig.Config{
		Version: nodeconfig.VersionV1,
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   server.URL,
			NetworkID: "local_lab",
		},
		Attachments: []nodeconfig.AttachmentConfig{
			{
				Agent: bridgeconfig.AgentConfig{ID: "codex_bot", Name: "Codex Bot"},
				Runtime: bridgeconfig.RuntimeConfig{
					Kind:             bridgeconfig.RuntimeCodex,
					Command:          writeNodeFakeRuntimeScript(t, codexLog, `{"session_id":"codex-session"}`),
					WorkspacePath:    codexWorkspace,
					SessionStorePath: filepath.Join(codexWorkspace, ".moltnet", "sessions.json"),
				},
				Rooms: []bridgeconfig.RoomBinding{{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto}},
			},
			{
				Agent: bridgeconfig.AgentConfig{ID: "claude_bot", Name: "Claude Bot"},
				Runtime: bridgeconfig.RuntimeConfig{
					Kind:             bridgeconfig.RuntimeClaudeCode,
					Command:          writeNodeFakeRuntimeScript(t, claudeLog, "claude stdout should be ignored"),
					WorkspacePath:    claudeWorkspace,
					SessionStorePath: filepath.Join(claudeWorkspace, ".moltnet", "sessions.json"),
				},
				Rooms: []bridgeconfig.RoomBinding{{ID: "research", Read: bridgeconfig.ReadMentions, Reply: bridgeconfig.ReplyAuto}},
			},
		},
	})
	if err != nil {
		t.Fatalf("node.New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("runner.Run() shutdown error = %v", err)
			}
		case <-time.After(cliRuntimeIntegrationWaitTimeout):
			t.Fatal("timed out waiting for node runner shutdown")
		}
	})

	waitForAgent(t, server.URL, "codex_bot")
	waitForAgent(t, server.URL, "claude_bot")

	postMessage(t, server.URL, "hello <@molt://local_lab/agents/codex_bot>")
	waitForFileContains(t, codexLog, "Message:\nhello <@molt://local_lab/agents/codex_bot>")
	waitForFileContains(t, codexLog, "DONE")
	assertFileNotContains(t, claudeLog, "codex_bot")

	postMessage(t, server.URL, "hello <@molt://local_lab/agents/claude_bot>")
	waitForFileContains(t, claudeLog, "Message:\nhello <@molt://local_lab/agents/claude_bot>")
	waitForFileContains(t, claudeLog, "DONE")

	codexLogText := readFile(t, codexLog)
	for _, want := range []string{
		"PWD=" + codexWorkspace,
		"ARGS=exec --json --skip-git-repo-check --output-last-message " + filepath.Join(codexWorkspace, ".moltnet", "codex-last-message.txt") + " -",
		"Channel: moltnet",
		"Chat ID: local_lab:room:research",
		"Mentions: molt://local_lab/agents/codex_bot",
	} {
		if !strings.Contains(codexLogText, want) {
			t.Fatalf("codex log missing %q:\n%s", want, codexLogText)
		}
	}

	claudeLogText := readFile(t, claudeLog)
	for _, want := range []string{
		"PWD=" + claudeWorkspace,
		"ARGS=--print --session-id ",
		"Channel: moltnet",
		"Chat ID: local_lab:room:research",
		"Mentions: molt://local_lab/agents/claude_bot",
	} {
		if !strings.Contains(claudeLogText, want) {
			t.Fatalf("claude log missing %q:\n%s", want, claudeLogText)
		}
	}

	messages := readRoomMessages(t, server.URL)
	if len(messages.Messages) != 2 {
		t.Fatalf("expected only the two explicit input messages, got %#v", messages.Messages)
	}
}

func writeNodeFakeRuntimeScript(t *testing.T, logPath string, stdout string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "runtime")
	script := "#!/bin/sh\n" +
		"printf 'PWD=%s\\n' \"$PWD\" >>" + shellEscapeForNodeTest(logPath) + "\n" +
		"printf 'ARGS=%s\\n' \"$*\" >>" + shellEscapeForNodeTest(logPath) + "\n" +
		"printf 'MOLTNET_CLIENT_CONFIG=%s\\n' \"$MOLTNET_CLIENT_CONFIG\" >>" + shellEscapeForNodeTest(logPath) + "\n" +
		"cat >>" + shellEscapeForNodeTest(logPath) + "\n" +
		"printf '\\n---\\n' >>" + shellEscapeForNodeTest(logPath) + "\n" +
		"printf '%s\\n' " + shellEscapeForNodeTest(stdout) + "\n" +
		"printf 'DONE\\n' >>" + shellEscapeForNodeTest(logPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

func waitForAgent(t *testing.T, baseURL string, agentID string) {
	t.Helper()

	deadline := time.Now().Add(cliRuntimeIntegrationWaitTimeout)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + "/v1/agents/" + agentID)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(cliRuntimeIntegrationPollInterval)
	}
	t.Fatalf("timed out waiting for agent %q", agentID)
}

func postMessage(t *testing.T, baseURL string, text string) {
	t.Helper()

	payload, err := json.Marshal(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "human", ID: "tester", Name: "Tester"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: text}},
	})
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	response, err := http.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post message: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected message status %d", response.StatusCode)
	}
}

func readRoomMessages(t *testing.T, baseURL string) protocol.MessagePage {
	t.Helper()

	response, err := http.Get(baseURL + "/v1/rooms/research/messages")
	if err != nil {
		t.Fatalf("read room messages: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected messages status %d", response.StatusCode)
	}

	var page protocol.MessagePage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode room messages: %v", err)
	}
	return page
}

func waitForFileContains(t *testing.T, path string, want string) {
	t.Helper()

	deadline := time.Now().Add(cliRuntimeIntegrationWaitTimeout)
	for time.Now().Before(deadline) {
		if strings.Contains(readFile(t, path), want) {
			return
		}
		time.Sleep(cliRuntimeIntegrationPollInterval)
	}
	t.Fatalf("timed out waiting for %q in %s; contents:\n%s", want, path, readFile(t, path))
}

func assertFileNotContains(t *testing.T, path string, needle string) {
	t.Helper()

	if strings.Contains(readFile(t, path), needle) {
		t.Fatalf("did not expect %q in %s:\n%s", needle, path, readFile(t, path))
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return string(bytes)
}

func shellEscapeForNodeTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
