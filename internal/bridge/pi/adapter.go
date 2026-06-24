package pi

import (
	"context"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Name() string {
	return bridgeconfig.RuntimePi
}

func (a *Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	if strings.TrimSpace(config.Runtime.ControlURL) == "" {
		return fmt.Errorf("pi adapter requires runtime.control_url")
	}

	return loop.RunControlLoop(ctx, config)
}
