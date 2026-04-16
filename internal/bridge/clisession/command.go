package clisession

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

const defaultCommandTimeout = 5 * time.Minute

type CommandSpec struct {
	Command string
	Args    []string
	Stdin   string
	Env     []string
	Dir     string
	Timeout time.Duration
}

type CommandResult struct {
	Stdout string
	Stderr string
}

func RunCommand(ctx context.Context, spec CommandSpec) (CommandResult, error) {
	commandPath := strings.TrimSpace(spec.Command)
	if commandPath == "" {
		return CommandResult{}, fmt.Errorf("runtime command is required")
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}

	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resolvedCommand, err := resolveCommandPath(commandPath)
	if err != nil {
		return CommandResult{}, err
	}

	cmd := exec.CommandContext(commandCtx, resolvedCommand, spec.Args...)
	cmd.Dir = strings.TrimSpace(spec.Dir)
	cmd.Env = append(cmd.Environ(), spec.Env...)
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
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
			return CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}, fmt.Errorf("run runtime command: %w: %s", err, stderrText)
		}
		return CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}, fmt.Errorf("run runtime command: %w", err)
	}

	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func BaseEnv(config bridgeconfig.Config) []string {
	env := []string{
		"MOLTNET_CLIENT_CONFIG=" + filepath.Join(strings.TrimSpace(config.Runtime.WorkspacePath), ".moltnet", "config.json"),
	}
	if homePath := strings.TrimSpace(config.Runtime.HomePath); homePath != "" {
		env = append(env, "HOME="+homePath)
	}
	return env
}

func resolveCommandPath(commandPath string) (string, error) {
	if filepath.IsAbs(commandPath) || !strings.ContainsRune(commandPath, os.PathSeparator) {
		return commandPath, nil
	}

	absolute, err := filepath.Abs(commandPath)
	if err != nil {
		return "", fmt.Errorf("resolve runtime command path: %w", err)
	}
	return absolute, nil
}
