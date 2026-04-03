package node

import (
	"context"
	"fmt"
	"sync"

	"github.com/noopolis/moltnet/internal/bridge/core"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
)

type attachmentRunner interface {
	Run(ctx context.Context) error
}

type runnerFactory func(config bridgeconfig.Config) (attachmentRunner, error)

type Runner struct {
	configs   []bridgeconfig.Config
	newRunner runnerFactory
}

func New(config nodeconfig.Config) (*Runner, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Runner{
		configs:   config.BridgeConfigs(),
		newRunner: newCoreRunner,
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if len(r.configs) == 0 {
		<-ctx.Done()
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errorCh := make(chan error, len(r.configs))
	var waitGroup sync.WaitGroup

	for _, config := range r.configs {
		runner, err := r.newRunner(config)
		if err != nil {
			return err
		}

		waitGroup.Add(1)
		go func(config bridgeconfig.Config, runner attachmentRunner) {
			defer waitGroup.Done()

			observability.Logger(
				ctx,
				"node",
				"agent_id", config.Agent.ID,
				"runtime", config.Runtime.Kind,
				"network_id", config.Moltnet.NetworkID,
			).Info("moltnet-node attachment starting")
			if err := runner.Run(ctx); err != nil {
				select {
				case errorCh <- fmt.Errorf("attachment %s: %w", config.Agent.ID, err):
					cancel()
				default:
				}
			}
		}(config, runner)
	}

	doneCh := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		<-doneCh
		select {
		case err := <-errorCh:
			return err
		default:
			return nil
		}
	case err := <-errorCh:
		<-doneCh
		return err
	}
}

type coreRunner struct {
	runner *core.Runner
}

func newCoreRunner(config bridgeconfig.Config) (attachmentRunner, error) {
	runner, err := core.New(config)
	if err != nil {
		return nil, err
	}

	return &coreRunner{runner: runner}, nil
}

func (r *coreRunner) Run(ctx context.Context) error {
	return r.runner.Run(ctx)
}
