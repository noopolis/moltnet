package picoclaw

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const picoCommandTimeout = 2 * time.Minute

func runCommandLoop(
	ctx context.Context,
	config bridgeconfig.Config,
	streamer moltnetClient,
	backoff backoffPolicy,
) error {
	attempt := 0

	for {
		if ctx.Err() != nil {
			return nil
		}

		err := streamer.StreamEvents(ctx, config, func(event protocol.Event) error {
			if !loop.ShouldHandle(config, event) {
				return nil
			}

			return sendEventCommand(ctx, config, streamer, event)
		})
		if err == nil || ctx.Err() != nil {
			return err
		}
		attempt++

		observability.Logger(ctx, "bridge.picoclaw", "agent_id", config.Agent.ID, "error", err).
			Warn("picoclaw bridge inbound stream error")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff.Delay(attempt)):
		}
	}
}

func sendEventCommand(ctx context.Context, config bridgeconfig.Config, client moltnetClient, event protocol.Event) error {
	if event.Message == nil {
		return fmt.Errorf("event has no message")
	}

	prompt := buildInboundMessage(config, event, true)
	toolMessages, err := runPicoCommand(
		ctx,
		config,
		picoSessionKey(config, event.Message),
		prompt,
		event.Message.Target,
	)
	if err != nil {
		return err
	}

	for _, message := range toolMessages {
		_, err := client.SendMessage(ctx, protocol.SendMessageRequest{
			Target: message.Target,
			From: protocol.Actor{
				Type: "agent",
				ID:   config.Agent.ID,
				Name: bridgeutil.DisplayName(config.Agent),
			},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: message.Content}},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func runPicoCommand(
	ctx context.Context,
	config bridgeconfig.Config,
	sessionID string,
	prompt string,
	fallbackTarget protocol.Target,
) ([]picoMoltnetToolMessage, error) {
	commandPath := strings.TrimSpace(config.Runtime.Command)
	if commandPath == "" {
		return nil, fmt.Errorf("picoclaw bridge requires runtime.command")
	}
	configPath := strings.TrimSpace(config.Runtime.ConfigPath)
	if configPath == "" {
		return nil, fmt.Errorf("picoclaw bridge requires runtime.config_path")
	}

	commandCtx, cancel := context.WithTimeout(ctx, picoCommandTimeout)
	defer cancel()

	sessionPath := picoSessionJSONLPath(config, sessionID)
	sessionOffset := picoSessionOffset(sessionPath)

	cmd := exec.CommandContext(
		commandCtx,
		commandPath,
		"agent",
		"--session",
		sessionID,
		"--message",
		prompt,
	)
	cmd.Env = append(cmd.Environ(), "PICOCLAW_CONFIG="+configPath)
	if homePath := strings.TrimSpace(config.Runtime.HomePath); homePath != "" {
		cmd.Env = append(
			cmd.Env,
			"HOME="+homePath,
			"PICOCLAW_HOME="+homePath,
			"CODEX_HOME="+filepath.Join(homePath, ".codex"),
		)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText == "" {
			stderrText = strings.TrimSpace(stdout.String())
		}
		if stderrText != "" {
			return nil, fmt.Errorf("run picoclaw command: %w: %s", err, stderrText)
		}
		return nil, fmt.Errorf("run picoclaw command: %w", err)
	}

	if stdoutText := strings.TrimSpace(stdout.String()); stdoutText != "" {
		observability.Logger(ctx, "bridge.picoclaw", "agent_id", config.Agent.ID, "output", stdoutText).
			Debug("picoclaw command completed")
	}

	return readPicoMoltnetToolMessages(sessionPath, sessionOffset, config.Moltnet.NetworkID, fallbackTarget)
}
