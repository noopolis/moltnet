package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/noopolis/moltnet/internal/bridge/core"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type attachmentRunner interface {
	Run(ctx context.Context) error
}

type runnerFactory func(config bridgeconfig.Config) (attachmentRunner, error)

type preflightFunc func(ctx context.Context, configs []bridgeconfig.Config) error

type Runner struct {
	configs   []bridgeconfig.Config
	newRunner runnerFactory
	preflight preflightFunc
}

func New(config nodeconfig.Config) (*Runner, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Runner{
		configs:   config.BridgeConfigs(),
		newRunner: newCoreRunner,
		preflight: preflightAttachments,
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if len(r.configs) == 0 {
		<-ctx.Done()
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if ctx.Err() != nil {
		return nil
	}
	preflight := r.preflight
	if preflight == nil {
		preflight = preflightAttachments
	}
	if err := preflight(ctx, r.configs); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

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

type preflightResult struct {
	networkErr error
	network    protocol.Network
}

func preflightAttachments(ctx context.Context, configs []bridgeconfig.Config) error {
	cache := make(map[core.PreflightCacheKey]preflightResult, len(configs))
	errs := make([]error, 0)

	for _, config := range configs {
		if ctx.Err() != nil {
			return nil
		}

		request, err := core.NewPreflightRequest(config)
		if err != nil {
			errs = append(errs, fmt.Errorf("attachment %s: %w", attachmentID(config), err))
			continue
		}

		key := request.CacheKey()
		result, ok := cache[key]
		if !ok {
			network, networkErr := core.FetchPreflightNetwork(ctx, request)
			result = preflightResult{networkErr: networkErr, network: network}
			cache[key] = result
		}
		if result.networkErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			errs = append(errs, fmt.Errorf("attachment %s: %w", attachmentID(config), result.networkErr))
			continue
		}

		if err := core.ValidateNetworkCompatibility(config, result.network); err != nil {
			errs = append(errs, fmt.Errorf("attachment %s: %w", attachmentID(config), err))
		}
	}

	return errors.Join(errs...)
}

func attachmentID(config bridgeconfig.Config) string {
	if agentID := strings.TrimSpace(config.Agent.ID); agentID != "" {
		return agentID
	}
	return "<unknown>"
}

type coreRunner struct {
	runner *core.Runner
}

func newCoreRunner(config bridgeconfig.Config) (attachmentRunner, error) {
	runner, err := core.NewWithPreflight(config, nil)
	if err != nil {
		return nil, err
	}

	return &coreRunner{runner: runner}, nil
}

func (r *coreRunner) Run(ctx context.Context) error {
	return r.runner.Run(ctx)
}
