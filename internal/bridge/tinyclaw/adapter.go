package tinyclaw

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

const (
	defaultChannelName = "moltnet"
	pollInterval       = 1 * time.Second
	reconnectDelay     = 2 * time.Second
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

	errorCh := make(chan error, 2)
	var once sync.Once
	report := func(err error) {
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}

		once.Do(func() {
			errorCh <- err
		})
	}

	go func() {
		report(bridge.runInbound(ctx))
	}()

	go func() {
		report(bridge.runOutbound(ctx))
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errorCh:
		return err
	}
}

type bridge struct {
	config       bridgeconfig.Config
	channel      string
	moltnet      *moltnetClient
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
		moltnet:      newMoltnetClient(config),
		tinyclaw:     newAPIClient(config),
		roomBindings: roomBindings,
		agentName:    agentName,
	}, nil
}

func (b *bridge) runInbound(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		err := b.moltnet.streamEvents(ctx, func(event moltnetEvent) error {
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

		log.Printf("tinyclaw bridge inbound stream error: %v", err)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(reconnectDelay):
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
				log.Printf("tinyclaw bridge outbound poll error: %v", err)
			}
		}
	}
}

func (b *bridge) flushResponses(ctx context.Context) error {
	responses, err := b.tinyclaw.pendingResponses(ctx)
	if err != nil {
		return err
	}

	for _, response := range responses {
		request, err := b.toMoltnetMessage(response)
		if err != nil {
			log.Printf("tinyclaw bridge skip invalid response %d: %v", response.ID, err)
			continue
		}

		if _, err := b.moltnet.sendMessage(ctx, request); err != nil {
			return err
		}

		if err := b.tinyclaw.ackResponse(ctx, response.ID); err != nil {
			return err
		}
	}

	return nil
}
