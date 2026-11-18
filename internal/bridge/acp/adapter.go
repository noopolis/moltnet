package acp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/clisession"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (*Adapter) Name() string {
	return bridgeconfig.RuntimeACP
}

func (*Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	executor, err := newExecutor(ctx, &config)
	if err != nil {
		return err
	}
	defer executor.close()

	runner := clisession.NewTurnRunner(
		config,
		executor,
		loop.NewMoltnetClient(config),
		bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay),
	)
	return runner.Run(ctx)
}

func newExecutor(ctx context.Context, config *bridgeconfig.Config) (*executor, error) {
	workspace := strings.TrimSpace(config.Runtime.WorkspacePath)
	if workspace == "" {
		return nil, fmt.Errorf("ACP adapter requires runtime.workspace_path")
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve ACP workspace: %w", err)
	}
	command := strings.TrimSpace(config.Runtime.Command)
	if command == "" {
		return nil, fmt.Errorf("ACP adapter requires runtime.command")
	}
	command, err = clisession.ResolveCommandPath(command)
	if err != nil {
		return nil, err
	}
	config.Runtime.WorkspacePath = abs
	if strings.TrimSpace(config.Runtime.SessionStorePath) == "" {
		config.Runtime.SessionStorePath = filepath.Join(abs, ".moltnet", "acp-sessions.json")
	}

	return &executor{
		lifetime:  ctx,
		startGate: make(chan struct{}, 1),
		spec: launchSpec{
			command: command,
			args:    config.Runtime.Args,
			dir:     abs,
			env:     clisession.BaseEnv(*config),
		},
	}, nil
}
