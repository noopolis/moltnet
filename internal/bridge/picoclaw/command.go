package picoclaw

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const picoCommandTimeout = 2 * time.Minute

func runCommandLoop(
	ctx context.Context,
	config bridgeconfig.Config,
	streamer eventStreamer,
	backoff backoffPolicy,
) error {
	attempt := 0
	bootstrapped := false

	for {
		if ctx.Err() != nil {
			return nil
		}

		if !bootstrapped {
			if err := sendBootstrapCommands(ctx, config); err != nil {
				attempt++
				observability.Logger(ctx, "bridge.picoclaw", "agent_id", config.Agent.ID, "error", err).
					Warn("picoclaw bridge bootstrap error")

				select {
				case <-ctx.Done():
					return nil
				case <-time.After(backoff.Delay(attempt)):
				}
				continue
			}
			bootstrapped = true
		}

		err := streamer.StreamEvents(ctx, config, func(event protocol.Event) error {
			if !loop.ShouldHandle(config, event) {
				return nil
			}

			return sendEventCommand(ctx, config, event)
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

func sendBootstrapCommands(ctx context.Context, config bridgeconfig.Config) error {
	for _, target := range bootstrapTargets(config) {
		prompt := buildBootstrapMessage(config, target, true)
		if err := runPicoCommand(
			ctx,
			config,
			picoSessionKeyForTarget(config, target),
			prompt,
		); err != nil {
			return err
		}
	}

	return nil
}

func sendEventCommand(ctx context.Context, config bridgeconfig.Config, event protocol.Event) error {
	if event.Message == nil {
		return fmt.Errorf("event has no message")
	}

	prompt := buildInboundMessage(config, event, true)
	return runPicoCommand(
		ctx,
		config,
		picoSessionKey(config, event.Message),
		prompt,
	)
}

func runPicoCommand(
	ctx context.Context,
	config bridgeconfig.Config,
	sessionID string,
	prompt string,
) error {
	commandPath := strings.TrimSpace(config.Runtime.Command)
	if commandPath == "" {
		return fmt.Errorf("picoclaw bridge requires runtime.command")
	}
	configPath := strings.TrimSpace(config.Runtime.ConfigPath)
	if configPath == "" {
		return fmt.Errorf("picoclaw bridge requires runtime.config_path")
	}

	commandCtx, cancel := context.WithTimeout(ctx, picoCommandTimeout)
	defer cancel()

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
		cmd.Env = append(cmd.Env, "HOME="+homePath, "PICOCLAW_HOME="+homePath)
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
			return fmt.Errorf("run picoclaw command: %w: %s", err, stderrText)
		}
		return fmt.Errorf("run picoclaw command: %w", err)
	}

	if stdoutText := strings.TrimSpace(stdout.String()); stdoutText != "" {
		observability.Logger(ctx, "bridge.picoclaw", "agent_id", config.Agent.ID, "output", stdoutText).
			Debug("picoclaw command completed")
	}

	return nil
}
