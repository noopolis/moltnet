package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/internal/bridge/clisession"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type Driver struct{}

func (Driver) Name() string {
	return bridgeconfig.RuntimeCodex
}

func (Driver) DefaultCommand() string {
	return "codex"
}

func (Driver) UsesSessionIDForFirstDelivery() bool {
	return false
}

func (Driver) BuildCommand(config bridgeconfig.Config, delivery clisession.Delivery) (clisession.CommandSpec, error) {
	command := strings.TrimSpace(config.Runtime.Command)
	if command == "" {
		command = "codex"
	}

	lastMessagePath := filepath.Join(strings.TrimSpace(config.Runtime.WorkspacePath), ".moltnet", "codex-last-message.txt")
	args := []string{"exec"}
	if delivery.ExistingSession {
		args = append(args, "resume")
	}
	args = append(args,
		"--json",
		"--skip-git-repo-check",
		"--output-last-message",
		lastMessagePath,
	)
	if delivery.ExistingSession {
		args = append(args, delivery.RuntimeSessionID)
	}
	args = append(args, "-")

	env := []string{}
	if homePath := strings.TrimSpace(config.Runtime.HomePath); homePath != "" {
		env = append(env, "CODEX_HOME="+filepath.Join(homePath, ".codex"))
	}

	return clisession.CommandSpec{
		Command: command,
		Args:    args,
		Stdin:   delivery.Prompt,
		Env:     env,
	}, nil
}

func (Driver) ExtractRuntimeSessionID(result clisession.CommandResult) string {
	for _, line := range strings.Split(result.Stdout, "\n") {
		if sessionID := extractSessionIDFromJSONLine(line); sessionID != "" {
			return sessionID
		}
	}
	return ""
}

func extractSessionIDFromJSONLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return ""
	}
	return findSessionID(payload)
}

func findSessionID(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"session_id", "sessionId", "conversation_id", "conversationId"} {
			if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
		if typeText, ok := typed["type"].(string); ok && strings.Contains(strings.ToLower(typeText), "session") {
			if text, ok := typed["id"].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
		for _, nested := range typed {
			if sessionID := findSessionID(nested); sessionID != "" {
				return sessionID
			}
		}
	case []any:
		for _, nested := range typed {
			if sessionID := findSessionID(nested); sessionID != "" {
				return sessionID
			}
		}
	}
	return ""
}
