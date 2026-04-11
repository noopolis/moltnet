package picoclaw

import (
	"context"
	"fmt"
	"strings"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Name() string {
	return bridgeconfig.RuntimePicoClaw
}

func (a *Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	if strings.TrimSpace(config.Runtime.Command) != "" {
		return runCommandLoop(
			ctx,
			config,
			loop.NewMoltnetClient(config),
			bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay),
		)
	}

	if strings.TrimSpace(config.Runtime.EventsURL) != "" {
		return runEventLoop(
			ctx,
			config,
			loop.NewMoltnetClient(config),
			bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay),
		)
	}

	if strings.TrimSpace(config.Runtime.ControlURL) == "" {
		return fmt.Errorf("picoclaw adapter requires runtime.control_url, runtime.events_url, or runtime.command")
	}

	return loop.RunControlLoop(ctx, config)
}
