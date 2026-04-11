package openclaw

import (
	"context"
	"fmt"
	"strings"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type eventStreamer interface {
	StreamEvents(
		ctx context.Context,
		config bridgeconfig.Config,
		handle func(event protocol.Event) error,
	) error
}

type backoffPolicy interface {
	Delay(attempt int) time.Duration
}

func runGatewayLoop(
	ctx context.Context,
	config bridgeconfig.Config,
	streamer eventStreamer,
	backoff backoffPolicy,
) error {
	attempt := 0
	bootstrapped := false

	for {
		if ctx.Err() != nil {
			return nil
		}

		if !bootstrapped {
			if err := sendBootstrapDispatches(ctx, config); err != nil {
				attempt++
				observability.Logger(ctx, "bridge.openclaw", "agent_id", config.Agent.ID, "error", err).
					Warn("openclaw bridge bootstrap error")

				select {
				case <-ctx.Done():
					return nil
				case <-time.After(backoff.Delay(attempt)):
				}
				continue
			}
			bootstrapped = true
		}

		err := streamer.StreamEvents(ctx, config, func(event protocol.Event) error {
			if !shouldDeliver(config, event) {
				return nil
			}
			return sendEventDispatch(ctx, config, event)
		})
		if err == nil || ctx.Err() != nil {
			return err
		}
		attempt++

		observability.Logger(ctx, "bridge.openclaw", "agent_id", config.Agent.ID, "error", err).
			Warn("openclaw bridge inbound stream error")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff.Delay(attempt)):
		}
	}
}

func shouldDeliver(config bridgeconfig.Config, event protocol.Event) bool {
	if event.Type != protocol.EventTypeMessageCreated || event.Message == nil {
		return false
	}

	message := event.Message
	if message.NetworkID != config.Moltnet.NetworkID ||
		protocol.ActorMatches(config.Moltnet.NetworkID, config.Agent.ID, message.From.ID) {
		return false
	}

	switch message.Target.Kind {
	case protocol.TargetKindRoom:
		return shouldDeliverRoom(config, message)
	case protocol.TargetKindDM:
		return shouldDeliverDirectMessage(config, message)
	default:
		return false
	}
}

func shouldDeliverRoom(config bridgeconfig.Config, message *protocol.Message) bool {
	if message == nil {
		return false
	}

	for _, binding := range config.Rooms {
		if binding.ID != message.Target.RoomID {
			continue
		}

		return bridgeutil.ShouldRead(binding.Read, message.Target, message.Mentions, config.Agent) &&
			binding.Reply != bridgeconfig.ReplyNever
	}

	return false
}

func shouldDeliverDirectMessage(config bridgeconfig.Config, message *protocol.Message) bool {
	if message == nil || config.DMs == nil || !config.DMs.Enabled {
		return false
	}
	if !bridgeutil.ShouldReadDirect(config.DMs.Read) || config.DMs.Reply == bridgeconfig.ReplyNever {
		return false
	}

	for _, participantID := range message.Target.ParticipantIDs {
		if protocol.ActorMatches(config.Moltnet.NetworkID, config.Agent.ID, participantID) ||
			participantID == config.Agent.Name {
			return true
		}
	}

	return false
}

func sendBootstrapDispatches(ctx context.Context, config bridgeconfig.Config) error {
	for _, target := range bootstrapTargets(config) {
		if err := sendGatewayChat(
			ctx,
			config,
			sessionKeyForTarget(config, target),
			buildBootstrapMessage(config, target),
			bootstrapIdempotencyKey(config, target),
		); err != nil {
			return err
		}
	}

	return nil
}

func bootstrapTargets(config bridgeconfig.Config) []protocol.Target {
	targets := make([]protocol.Target, 0, len(config.Rooms))
	for _, binding := range config.Rooms {
		if strings.TrimSpace(binding.ID) == "" ||
			binding.Reply == bridgeconfig.ReplyNever ||
			binding.Read == bridgeconfig.ReadThreadOnly ||
			binding.Read == bridgeconfig.ReadMentions {
			continue
		}
		targets = append(targets, protocol.Target{
			Kind:   protocol.TargetKindRoom,
			RoomID: binding.ID,
		})
	}

	return targets
}

func sendEventDispatch(ctx context.Context, config bridgeconfig.Config, event protocol.Event) error {
	if event.Message == nil {
		return fmt.Errorf("event has no message")
	}

	message, err := buildInboundMessage(config, event)
	if err != nil {
		return err
	}

	return sendGatewayChat(
		ctx,
		config,
		sessionKey(config, event.Message),
		message,
		idempotencyKey(config, event),
	)
}
