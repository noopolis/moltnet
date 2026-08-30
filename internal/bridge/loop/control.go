package loop

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const controlRequestTimeout = 5 * time.Minute

type bootstrapTarget struct {
	message string
	target  protocol.Target
}

func RunControlLoop(ctx context.Context, config bridgeconfig.Config) error {
	return RunControlLoopWithCodec(ctx, config, &legacyControlCodec{})
}

func RunControlLoopWithCodec(ctx context.Context, config bridgeconfig.Config, codec ControlCodec) error {
	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()

	client := NewMoltnetClient(config)
	if asyncCodec, ok := codec.(AsyncControlCodec); ok {
		if err := asyncCodec.StartControlAsync(loopCtx, client, config); err != nil {
			return err
		}
		// Cancel BEFORE waiting, and register this after `defer cancelLoop()`
		// so LIFO ordering runs it first: waiting on a follower whose context
		// is still live would hang instead of shutting down.
		defer func() {
			cancelLoop()
			asyncCodec.WaitControlAsync()
		}()
	}
	controlClient := &http.Client{Timeout: controlRequestTimeout}
	backoff := bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay)
	attempt := 0
	bootstrapped := false
	// Created once, outside the reconnect loop below, so a permanently
	// failing event.ID stays known across reconnects instead of every
	// reconnect restarting its retry budget at zero (the structural cause
	// of the retry-storm this loop used to be able to produce).
	deliveries := newControlDeliveryTracker()

	for {
		if ctx.Err() != nil {
			return nil
		}

		streamCtx, cancelStream := context.WithCancel(loopCtx)
		bootstrapDone := make(chan error, 1)
		bootstrapStarted := false

		err := client.StreamEventsReady(streamCtx, config, func() {
			if bootstrapped || bootstrapStarted {
				return
			}
			bootstrapStarted = true

			go func() {
				err := sendBootstrapControlMessages(streamCtx, controlClient, config, codec)
				if err != nil {
					bootstrapDone <- err
					cancelStream()
					return
				}
				bootstrapDone <- nil
			}()
		}, func(event protocol.Event) error {
			if bootstrapErr, ok := readBootstrapResult(bootstrapDone); ok {
				bootstrapStarted = false
				if bootstrapErr != nil {
					return bootstrapErr
				}
				bootstrapped = true
				attempt = 0
			}

			if !ShouldHandle(config, event) {
				return nil
			}

			return deliverControlMessage(ctx, controlClient, client, config, codec, event, deliveries)
		})
		cancelStream()

		if bootstrapErr, ok := readBootstrapResult(bootstrapDone); ok {
			bootstrapStarted = false
			if bootstrapErr != nil {
				err = bootstrapErr
			} else {
				bootstrapped = true
				attempt = 0
			}
		}

		if err == nil || ctx.Err() != nil {
			return err
		}
		attempt++

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff.Delay(attempt)):
		}
	}
}

func readBootstrapResult(results <-chan error) (error, bool) {
	select {
	case err := <-results:
		return err, true
	default:
		return nil, false
	}
}

func sendBootstrapControlMessages(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
	codec ControlCodec,
) error {
	for _, target := range bootstrapTargets(config) {
		_, err := sendControlText(
			ctx,
			controlClient,
			config,
			target.target,
			"", // bootstrap sends have no inbound moltnet message to derive an event id from
			"Moltnet Bootstrap",
			target.message,
			"",
			time.Time{},
			codec,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func bootstrapTargets(config bridgeconfig.Config) []bootstrapTarget {
	targets := make([]bootstrapTarget, 0, len(config.Rooms))

	for _, binding := range config.Rooms {
		if !bridgeutil.ShouldBootstrap(binding.Wake) {
			continue
		}

		target := protocol.Target{
			Kind:   protocol.TargetKindRoom,
			RoomID: binding.ID,
		}

		targets = append(targets, bootstrapTarget{
			message: buildBootstrapControlMessage(config, target),
			target:  target,
		})
	}

	return targets
}

func sendControlMessage(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
	event protocol.Event,
) (ControlResult, error) {
	return sendControlMessageWithCodec(ctx, controlClient, config, event, &legacyControlCodec{})
}

func sendControlMessageWithCodec(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
	event protocol.Event,
	codec ControlCodec,
) (ControlResult, error) {
	if event.Type != protocol.EventTypeMessageCreated {
		return ControlResult{}, fmt.Errorf("control wake requires %s event, got %s", protocol.EventTypeMessageCreated, event.Type)
	}
	if event.Message == nil {
		return ControlResult{}, fmt.Errorf("event has no message")
	}
	occurredAt := event.Message.CreatedAt
	if occurredAt.IsZero() {
		occurredAt = event.CreatedAt
	}

	return sendControlText(
		ctx,
		controlClient,
		config,
		event.Message.Target,
		protocol.MessageEventID(event.Message.ID),
		// Control attribution is an authenticated identity, not a display
		// string. Actor.ID is credential-bound by the server before this
		// stored event reaches the bridge. The protocol validation on that
		// path enforces [A-Za-z0-9][A-Za-z0-9._:-]{0,127}, so this derived
		// header value cannot contain a newline, space, or bracket.
		event.Message.From.ID,
		bridgeutil.RenderInboundText(event.Message),
		bridgeutil.RenderMessageBody(event.Message),
		occurredAt,
		codec,
	)
}

func sendControlText(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
	target protocol.Target,
	eventID string,
	from string,
	message string,
	transportText string,
	occurredAt time.Time,
	codec ControlCodec,
) (ControlResult, error) {
	if codec == nil {
		return ControlResult{}, fmt.Errorf("control codec is required")
	}
	delivery := ControlDelivery{
		Target:        target,
		EventID:       eventID,
		From:          from,
		Message:       message,
		TransportText: transportText,
		OccurredAt:    occurredAt,
	}
	request, err := codec.EncodeRequest(ctx, config, delivery)
	if err != nil {
		return ControlResult{}, err
	}
	if request == nil {
		return ControlResult{}, nil
	}

	response, err := controlClient.Do(request)
	if err != nil {
		return ControlResult{}, fmt.Errorf("request control url %s: %w", request.URL.Redacted(), err)
	}
	defer response.Body.Close()

	return codec.DecodeResponse(config, delivery, response)
}

func publishControlResult(
	ctx context.Context,
	client *MoltnetClient,
	config bridgeconfig.Config,
	target protocol.Target,
	inboundMessageID string,
	result ControlResult,
) (bool, error) {
	if !result.Publish {
		return false, nil
	}
	message := strings.TrimSpace(result.Text)
	if message == "" {
		return false, nil
	}
	_, err := client.SendMessage(ctx, protocol.SendMessageRequest{
		Target: target,
		From: protocol.Actor{
			Type: "agent",
			ID:   config.Agent.ID,
			Name: bridgeutil.DisplayName(config.Agent),
		},
		Parts:         []protocol.Part{{Kind: protocol.PartKindText, Text: message}},
		CauseEventIDs: []string{protocol.MessageEventID(inboundMessageID)},
	})
	return err == nil, err
}

func buildBootstrapControlMessage(config bridgeconfig.Config, target protocol.Target) string {
	lines := []string{
		"Moltnet bootstrap delivery.",
		"This is a live wake for the attached Moltnet conversation.",
		"You may stay silent, but if your own instructions define a startup action for an empty room, execute that startup action now instead of waiting for another prompt.",
		"The Moltnet CLI contract is already installed in your workspace.",
		"Do not answer this bootstrap with a status summary.",
	}
	if config.Runtime.Kind == bridgeconfig.RuntimeDaimon {
		lines = append(lines,
			"Your final response to this wake will be published automatically to the original Moltnet target.",
			"Use your final response to reply and mention colleagues when needed; return only the message to publish, not process notes or a promise to send it later.",
			"Do not use `moltnet send`, the Moltnet CLI, or another Moltnet messaging tool for this reply unless your own instructions independently configure a separate message.",
		)
	} else {
		lines = append(lines, "Nothing will be sent automatically from this wake. If you choose to act, you must run the tool or command that sends the message yourself.")
	}
	lines = append(lines,
		"If your own instructions say to coordinate privately, direct other agents, or never speak publicly, obey those local instructions.",
		"Read recent Moltnet history for the attached target. If the room is empty, it is appropriate to start it according to your local instructions, and you should do that in this wake.",
	)
	if config.Runtime.Kind != bridgeconfig.RuntimeDaimon {
		lines = append(lines, "If you decide to speak, use the exec tool with `moltnet send --target ... --text ...` and choose an explicit target.")
	}
	lines = append(lines, "",
		fmt.Sprintf(`{"kind":"bootstrap","source":"moltnet","network_id":%q,"conversation":%q,"target":{"kind":%q,"room_id":%q}}`,
			config.Moltnet.NetworkID,
			conversationContextIDForTarget(config.Moltnet.NetworkID, target),
			target.Kind,
			target.RoomID,
		),
	)
	return strings.Join(lines, "\n")
}
