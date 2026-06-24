package picoclaw

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunCommandLoopDeliversInboundMessagesWithoutBlockingBootstrap(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	homePath := filepath.Join(tempDir, "picoclaw")
	logPath := filepath.Join(tempDir, "picoclaw-command.log")
	sessionPath := filepath.Join(homePath, "workspace", "sessions", "agent_reviewer_local_room_research.jsonl")
	scriptPath := filepath.Join(tempDir, "picoclaw-agent")
	script := "#!/bin/sh\n" +
		"session=''\n" +
		"message=''\n" +
		"while [ \"$#\" -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    --session)\n" +
		"      session=\"$2\"\n" +
		"      shift 2\n" +
		"      ;;\n" +
		"    --message)\n" +
		"      message=\"$2\"\n" +
		"      shift 2\n" +
		"      ;;\n" +
		"    *)\n" +
		"      shift\n" +
		"      ;;\n" +
		"  esac\n" +
		"done\n" +
		"cat >>" + shellEscapeForTest(logPath) + " <<EOF\n" +
		"SESSION=${session}\n" +
		"CONFIG=${PICOCLAW_CONFIG}\n" +
		"HOME=${PICOCLAW_HOME}\n" +
		"CODEX_HOME=${CODEX_HOME}\n" +
		"MESSAGE=${message}\n" +
		"---\n" +
		"EOF\n" +
		"mkdir -p " + shellEscapeForTest(filepath.Dir(sessionPath)) + "\n" +
		"cat >>" + shellEscapeForTest(sessionPath) + " <<'JSON'\n" +
		"{\"role\":\"assistant\",\"content\":\"\",\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"message\",\"arguments\":\"{\\\"channel\\\":\\\"moltnet\\\",\\\"chat_id\\\":\\\"local:room:research\\\",\\\"content\\\":\\\"@writer pico reviewed\\\"}\"}}]}\n" +
		"JSON\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "reviewer", Name: "Reviewer"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://moltnet",
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			Command:    scriptPath,
			ConfigPath: "/tmp/picoclaw/config.json",
			HomePath:   homePath,
			Kind:       bridgeconfig.RuntimePicoClaw,
		},
		Rooms: []bridgeconfig.RoomBinding{
			{ID: "research", Wake: bridgeconfig.WakeAll},
		},
	}

	event := protocol.Event{
		ID:        "evt_123",
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: "local",
		Message: &protocol.Message{
			ID:        "msg_123",
			NetworkID: "local",
			Target: protocol.Target{
				Kind:   protocol.TargetKindRoom,
				RoomID: "research",
			},
			From: protocol.Actor{Type: "agent", ID: "writer", Name: "Writer"},
			Parts: []protocol.Part{
				{Kind: protocol.PartKindText, Text: "Review this patch"},
			},
		},
	}
	sent := make(chan protocol.SendMessageRequest, 1)

	if err := runCommandLoop(
		context.Background(),
		config,
		streamerStub{events: []protocol.Event{event}, sent: sent},
		backoffStub{},
	); err != nil {
		t.Fatalf("runCommandLoop() error = %v", err)
	}

	bytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read command log: %v", err)
	}
	logText := string(bytes)

	if strings.Count(logText, "SESSION=") != 1 {
		t.Fatalf("expected one command invocation, got log:\n%s", logText)
	}
	if !strings.Contains(logText, "SESSION=agent:reviewer:local:room:research") {
		t.Fatalf("expected stable session key, got log:\n%s", logText)
	}
	if !strings.Contains(logText, "CONFIG=/tmp/picoclaw/config.json") {
		t.Fatalf("expected config path env, got log:\n%s", logText)
	}
	if !strings.Contains(logText, "HOME="+homePath) {
		t.Fatalf("expected home path env, got log:\n%s", logText)
	}
	if !strings.Contains(logText, "CODEX_HOME="+filepath.Join(homePath, ".codex")) {
		t.Fatalf("expected codex home env, got log:\n%s", logText)
	}
	if !strings.Contains(logText, "Channel: moltnet") {
		t.Fatalf("expected compact channel header in message body, got log:\n%s", logText)
	}
	if !strings.Contains(logText, "Chat ID: local:room:research") {
		t.Fatalf("expected compact chat id in message body, got log:\n%s", logText)
	}
	if strings.Contains(logText, "Moltnet conversation attached.") {
		t.Fatalf("expected command mode to skip blocking bootstrap, got log:\n%s", logText)
	}
	if !strings.Contains(logText, "From: local/agent/writer") {
		t.Fatalf("expected sender metadata in log:\n%s", logText)
	}
	if !strings.Contains(logText, "Message:\nReview this patch") {
		t.Fatalf("expected compact message body in log:\n%s", logText)
	}

	select {
	case published := <-sent:
		if published.Target.Kind != protocol.TargetKindRoom || published.Target.RoomID != "research" {
			t.Fatalf("unexpected published target %#v", published.Target)
		}
		if published.From.ID != "reviewer" || published.From.Name != "Reviewer" {
			t.Fatalf("unexpected published actor %#v", published.From)
		}
		if len(published.Parts) != 1 || published.Parts[0].Text != "@writer pico reviewed" {
			t.Fatalf("unexpected published parts %#v", published.Parts)
		}
	default:
		t.Fatal("expected PicoClaw moltnet message tool call to be published")
	}
}

func shellEscapeForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
