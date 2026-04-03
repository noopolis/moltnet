package core

import (
	"context"
	"fmt"

	"github.com/noopolis/moltnet/internal/bridge/openclaw"
	"github.com/noopolis/moltnet/internal/bridge/picoclaw"
	"github.com/noopolis/moltnet/internal/bridge/tinyclaw"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type RuntimeAdapter interface {
	Name() string
	Run(ctx context.Context, config bridgeconfig.Config) error
}

type Runner struct {
	config  bridgeconfig.Config
	adapter RuntimeAdapter
}

func New(config bridgeconfig.Config) (*Runner, error) {
	adapter, err := selectAdapter(config.Runtime.Kind)
	if err != nil {
		return nil, err
	}

	return &Runner{
		config:  config,
		adapter: adapter,
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	observability.Logger(
		ctx,
		"bridge.core",
		"runtime", r.adapter.Name(),
		"agent_id", r.config.Agent.ID,
		"network_id", r.config.Moltnet.NetworkID,
		"moltnet_url", observability.RedactURL(r.config.Moltnet.BaseURL),
	).Info("moltnet-bridge starting")

	return r.adapter.Run(ctx, r.config)
}

func selectAdapter(kind string) (RuntimeAdapter, error) {
	switch kind {
	case bridgeconfig.RuntimeTinyClaw:
		return tinyclaw.New(), nil
	case bridgeconfig.RuntimeOpenClaw:
		return openclaw.New(), nil
	case bridgeconfig.RuntimePicoClaw:
		return picoclaw.New(), nil
	default:
		return nil, fmt.Errorf("unsupported runtime adapter %q", kind)
	}
}
