package tinyclaw

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	defaultChannelName  = "moltnet"
	pollInterval        = 1 * time.Second
	maxResponsesPerPoll = 20
)

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Name() string {
	return bridgeconfig.RuntimeTinyClaw
}

func (a *Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	if config.Runtime.ControlURL != "" {
		return loop.RunControlLoop(ctx, config)
	}

	bridge, err := newBridge(config)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)

	errorCh := make(chan error, 2)
	var once sync.Once
	report := func(err error) {
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}

		once.Do(func() {
			cancel()
			errorCh <- err
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		report(bridge.runInbound(runCtx))
	}()

	go func() {
		defer wg.Done()
		report(bridge.runOutbound(runCtx))
	}()

	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()

	select {
	case <-ctx.Done():
		cancel()
		<-waitCh
		return nil
	case err := <-errorCh:
		cancel()
		<-waitCh
		return err
	}
}

type bridge struct {
	config       bridgeconfig.Config
	channel      string
	moltnet      *loop.MoltnetClient
	tinyclaw     *apiClient
	roomBindings map[string]bridgeconfig.RoomBinding
	agentName    string
}

func newBridge(config bridgeconfig.Config) (*bridge, error) {
	if config.Runtime.InboundURL == "" {
		return nil, fmt.Errorf("tinyclaw bridge requires runtime.inbound_url")
	}

	if config.Runtime.OutboundURL == "" {
		return nil, fmt.Errorf("tinyclaw bridge requires runtime.outbound_url")
	}

	if config.Runtime.AckURL == "" {
		return nil, fmt.Errorf("tinyclaw bridge requires runtime.ack_url")
	}

	channel := config.Runtime.Channel
	if channel == "" {
		channel = defaultChannelName
	}

	roomBindings := make(map[string]bridgeconfig.RoomBinding, len(config.Rooms))
	for _, binding := range config.Rooms {
		roomBindings[binding.ID] = binding
	}

	agentName := config.Agent.Name
	if agentName == "" {
		agentName = config.Agent.ID
	}

	return &bridge{
		config:       config,
		channel:      channel,
		moltnet:      loop.NewMoltnetClient(config),
		tinyclaw:     newAPIClient(config),
		roomBindings: roomBindings,
		agentName:    agentName,
	}, nil
}

func (b *bridge) runInbound(ctx context.Context) error {
	backoff := bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay)
	attempt := 0

	for {
		if ctx.Err() != nil {
			return nil
		}

		err := b.moltnet.StreamEvents(ctx, b.config, func(event protocol.Event) error {
			if !b.shouldHandle(event) {
				return nil
			}

			request, err := b.toTinyClawMessage(event)
			if err != nil {
				return err
			}

			return b.tinyclaw.postMessage(ctx, request)
		})
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		attempt++

		observability.Logger(ctx, "bridge.tinyclaw", "agent_id", b.config.Agent.ID, "error", err).
			Warn("tinyclaw bridge inbound stream error")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff.Delay(attempt)):
		}
	}
}

func (b *bridge) runOutbound(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := b.flushResponses(ctx); err != nil {
				observability.Logger(ctx, "bridge.tinyclaw", "agent_id", b.config.Agent.ID, "error", err).
					Warn("tinyclaw bridge outbound poll error")
			}
		}
	}
}

func (b *bridge) flushResponses(ctx context.Context) error {
	responses, err := b.tinyclaw.pendingResponses(ctx)
	if err != nil {
		return err
	}

	limit := len(responses)
	if limit > maxResponsesPerPoll {
		limit = maxResponsesPerPoll
	}

	for _, response := range responses[:limit] {
		if err := b.tinyclaw.ackResponse(ctx, response.ID); err != nil {
			return err
		}
	}

	return nil
}
