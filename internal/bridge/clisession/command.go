package clisession

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

const defaultCommandTimeout = 5 * time.Minute
const textFileBusyRetries = 5

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

func (r *Runner) commandSpec(delivery Delivery) (CommandSpec, error) {
	spec, err := r.driver.BuildCommand(r.config, delivery)
	if err != nil {
		return CommandSpec{}, err
	}
	if strings.TrimSpace(spec.Command) == "" {
		spec.Command = r.driver.DefaultCommand()
	}
	if strings.TrimSpace(spec.Dir) == "" {
		spec.Dir = strings.TrimSpace(r.config.Runtime.WorkspacePath)
	}
	spec.Env = append(BaseEnv(r.config), spec.Env...)
	return spec, nil
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

	result, err := runResolvedCommand(commandCtx, resolvedCommand, spec)
	for attempt := 0; isTextFileBusy(err) && attempt < textFileBusyRetries; attempt++ {
		delay := time.Duration(attempt+1) * 10 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-commandCtx.Done():
			timer.Stop()
			return result, fmt.Errorf("run runtime command: %w", commandCtx.Err())
		case <-timer.C:
		}
		result, err = runResolvedCommand(commandCtx, resolvedCommand, spec)
	}
	if err != nil {
		return result, formatCommandError(result, err)
	}

	return result, nil
}

func runResolvedCommand(ctx context.Context, resolvedCommand string, spec CommandSpec) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, resolvedCommand, spec.Args...)
	cmd.Dir = strings.TrimSpace(spec.Dir)
	cmd.Env = append(cmd.Environ(), spec.Env...)
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

func formatCommandError(result CommandResult, err error) error {
	stderrText := strings.TrimSpace(result.Stderr)
	if stderrText == "" {
		stderrText = strings.TrimSpace(result.Stdout)
	}
	if stderrText != "" {
		return fmt.Errorf("run runtime command: %w: %s", err, stderrText)
	}
	return fmt.Errorf("run runtime command: %w", err)
}

func isTextFileBusy(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.ETXTBSY) ||
		strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "text file busy")
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
