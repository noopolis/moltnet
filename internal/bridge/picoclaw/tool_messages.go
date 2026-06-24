package picoclaw

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const maxPicoSessionLineBytes = 1 << 20

type picoMoltnetToolMessage struct {
	Target  protocol.Target
	Content string
}

type picoSessionEntry struct {
	ToolCalls []picoToolCall `json:"tool_calls,omitempty"`
}

type picoToolCall struct {
	Function picoToolFunction `json:"function"`
}

type picoToolFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type picoMessageToolArgs struct {
	Channel string `json:"channel"`
	ChatID  string `json:"chat_id"`
	Content string `json:"content"`
}

func picoSessionJSONLPath(config bridgeconfig.Config, sessionID string) string {
	homePath := strings.TrimSpace(config.Runtime.HomePath)
	if homePath == "" {
		return ""
	}
	return filepath.Join(homePath, "workspace", "sessions", picoSessionFilename(sessionID)+".jsonl")
}

func picoSessionFilename(sessionID string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_")
	return replacer.Replace(sessionID)
}

func picoSessionOffset(path string) int64 {
	if strings.TrimSpace(path) == "" {
		return 0
	}
	stat, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return stat.Size()
}

func readPicoMoltnetToolMessages(
	path string,
	offset int64,
	networkID string,
	fallbackTarget protocol.Target,
) ([]picoMoltnetToolMessage, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}

	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open picoclaw session JSONL: %w", err)
	}
	defer file.Close()

	if offset > 0 {
		if _, err := file.Seek(offset, 0); err != nil {
			return nil, fmt.Errorf("seek picoclaw session JSONL: %w", err)
		}
	}

	var messages []picoMoltnetToolMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxPicoSessionLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		extracted, err := extractPicoMoltnetToolMessages(line, networkID, fallbackTarget)
		if err != nil {
			return nil, err
		}
		messages = append(messages, extracted...)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan picoclaw session JSONL: %w", err)
	}

	return messages, nil
}

func extractPicoMoltnetToolMessages(
	line string,
	networkID string,
	fallbackTarget protocol.Target,
) ([]picoMoltnetToolMessage, error) {
	var entry picoSessionEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil, fmt.Errorf("decode picoclaw session JSONL line: %w", err)
	}

	var messages []picoMoltnetToolMessage
	for _, toolCall := range entry.ToolCalls {
		if toolCall.Function.Name != "message" {
			continue
		}

		args, err := decodePicoMessageToolArgs(toolCall.Function.Arguments)
		if err != nil {
			return nil, err
		}
		if channel := strings.TrimSpace(args.Channel); channel != "" && channel != "moltnet" {
			continue
		}

		content := strings.TrimSpace(args.Content)
		if content == "" {
			continue
		}

		target, ok := picoTargetFromChatID(networkID, args.ChatID, fallbackTarget)
		if !ok {
			continue
		}
		messages = append(messages, picoMoltnetToolMessage{Target: target, Content: content})
	}

	return messages, nil
}

func decodePicoMessageToolArgs(raw json.RawMessage) (picoMessageToolArgs, error) {
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		raw = json.RawMessage(encoded)
	}

	var args picoMessageToolArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, fmt.Errorf("decode picoclaw message tool args: %w", err)
	}
	return args, nil
}

func picoTargetFromChatID(
	networkID string,
	chatID string,
	fallbackTarget protocol.Target,
) (protocol.Target, bool) {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return fallbackTarget, fallbackTarget.Kind != ""
	}

	if prefix := strings.TrimSpace(networkID) + ":"; strings.HasPrefix(trimmed, prefix) {
		trimmed = strings.TrimPrefix(trimmed, prefix)
	}

	kind, id, ok := strings.Cut(trimmed, ":")
	if !ok {
		return protocol.Target{}, false
	}

	switch kind {
	case protocol.TargetKindRoom:
		if strings.TrimSpace(id) == "" {
			return protocol.Target{}, false
		}
		return protocol.Target{Kind: protocol.TargetKindRoom, RoomID: id}, true
	case protocol.TargetKindDM:
		if fallbackTarget.Kind == protocol.TargetKindDM && fallbackTarget.DMID == id {
			return fallbackTarget, true
		}
		return protocol.Target{}, false
	default:
		return protocol.Target{}, false
	}
}
