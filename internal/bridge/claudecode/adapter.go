package claudecode

import (
	"context"
	"fmt"
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

func (a *Adapter) Name() string {
	return bridgeconfig.RuntimeClaudeCode
}

func (a *Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	if strings.TrimSpace(config.Runtime.WorkspacePath) == "" {
		return fmt.Errorf("claude-code adapter requires runtime.workspace_path")
	}

	return clisession.Run(
		ctx,
		config,
		Driver{},
		loop.NewMoltnetClient(config),
		bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay),
	)
}
