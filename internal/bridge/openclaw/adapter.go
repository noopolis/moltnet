package openclaw

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

const hookRequestTimeout = 15 * time.Second

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Name() string {
	return bridgeconfig.RuntimeOpenClaw
}

func (a *Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	if strings.TrimSpace(config.Runtime.ControlURL) == "" {
		return fmt.Errorf("openclaw adapter requires runtime.control_url")
	}

	return runHookLoop(
		ctx,
		config,
		loop.NewMoltnetClient(config),
		&http.Client{Timeout: hookRequestTimeout},
		bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay),
	)
}
