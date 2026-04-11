package openclaw

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
	return bridgeconfig.RuntimeOpenClaw
}

func (a *Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	if strings.TrimSpace(config.Runtime.GatewayURL) == "" {
		return fmt.Errorf("openclaw adapter requires runtime.gateway_url")
	}

	return runGatewayLoop(
		ctx,
		config,
		loop.NewMoltnetClient(config),
		bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay),
	)
}
