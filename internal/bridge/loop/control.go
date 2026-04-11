package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const controlRequestTimeout = 15 * time.Second
const maxControlResponseBytes = 1 << 20

type controlRequest struct {
	ContextID string `json:"context_id,omitempty"`
	From      string `json:"from"`
	Message   string `json:"message"`
	To        string `json:"to"`
}

type controlResponse struct {
	From    string `json:"from"`
	Message string `json:"message"`
}

type bootstrapTarget struct {
	message string
	target  protocol.Target
}

func RunControlLoop(ctx context.Context, config bridgeconfig.Config) error {
	client := NewMoltnetClient(config)
	controlClient := &http.Client{Timeout: controlRequestTimeout}
	backoff := bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay)
	attempt := 0
	bootstrapped := false

	for {
		if ctx.Err() != nil {
			return nil
		}

		streamCtx, cancelStream := context.WithCancel(ctx)
		bootstrapDone := make(chan error, 1)
		bootstrapStarted := false

		err := client.StreamEventsReady(streamCtx, config, func() {
			if bootstrapped || bootstrapStarted {
				return
			}
			bootstrapStarted = true

			go func() {
				err := sendBootstrapControlMessages(streamCtx, client, controlClient, config)
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

			response, err := sendControlMessage(ctx, controlClient, config, event)
			if err != nil {
				return err
			}

			return publishControlResponse(ctx, client, config, event.Message.Target, response.Message)
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
	client *MoltnetClient,
	controlClient *http.Client,
	config bridgeconfig.Config,
) error {
	for _, target := range bootstrapTargets(config) {
		response, err := sendControlText(
			ctx,
			controlClient,
			config,
			target.target,
			"Moltnet Bootstrap",
			target.message,
		)
		if err != nil {
			return err
		}

		if err := publishControlResponse(ctx, client, config, target.target, response.Message); err != nil {
			return err
		}
	}

	return nil
}

func bootstrapTargets(config bridgeconfig.Config) []bootstrapTarget {
	targets := make([]bootstrapTarget, 0, len(config.Rooms))

	for _, binding := range config.Rooms {
		if !bridgeutil.ShouldReply(binding.Reply) {
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
) (controlResponse, error) {
	if event.Message == nil {
		return controlResponse{}, fmt.Errorf("event has no message")
	}

	return sendControlText(
		ctx,
		controlClient,
		config,
		event.Message.Target,
		bridgeutil.SenderName(event.Message.From),
		bridgeutil.RenderInboundText(event.Message),
	)
}

func sendControlText(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
	target protocol.Target,
	from string,
	message string,
) (controlResponse, error) {
	contextID := conversationContextIDForTarget(config.Moltnet.NetworkID, target)

	body, err := json.Marshal(controlRequest{
		ContextID: contextID,
		From:      from,
		Message:   message,
		To:        config.Agent.ID,
	})
	if err != nil {
		return controlResponse{}, fmt.Errorf("encode control request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		config.Runtime.ControlURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return controlResponse{}, fmt.Errorf("build control request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(config.Runtime.Token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := controlClient.Do(request)
	if err != nil {
		return controlResponse{}, fmt.Errorf("request control url %s: %w", request.URL.Redacted(), err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return controlResponse{}, fmt.Errorf("control url returned %s", response.Status)
	}

	var payload controlResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, maxControlResponseBytes)).Decode(&payload); err != nil {
		return controlResponse{}, fmt.Errorf("decode control response: %w", err)
	}

	return payload, nil
}

func publishControlResponse(
	ctx context.Context,
	client *MoltnetClient,
	config bridgeconfig.Config,
	target protocol.Target,
	message string,
) error {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return nil
	}

	_, err := client.SendMessage(ctx, protocol.SendMessageRequest{
		From: protocol.Actor{
			Type: "agent",
			ID:   config.Agent.ID,
			Name: bridgeutil.DisplayName(config.Agent),
		},
		Mentions: bridgeutil.ParseMentions(message),
		Parts: []protocol.Part{
			{
				Kind: protocol.PartKindText,
				Text: message,
			},
		},
		Target: target,
	})
	return err
}

func buildBootstrapControlMessage(config bridgeconfig.Config, target protocol.Target) string {
	return strings.Join([]string{
		"Moltnet bootstrap delivery.",
		"This is a live wake for the attached Moltnet conversation.",
		"You may stay silent, but if your own instructions define a startup action for an empty room, execute that startup action now instead of waiting for another prompt.",
		"The Moltnet CLI contract is already installed in your workspace.",
		"Do not answer this bootstrap with a status summary.",
		"Nothing will be sent automatically from this wake. If you choose to act, you must run the tool or command that sends the message yourself.",
		"If your own instructions say to coordinate privately, direct other agents, or never speak publicly, obey those local instructions.",
		"Read recent Moltnet history for the attached target. If the room is empty, it is appropriate to start it according to your local instructions, and you should do that in this wake.",
		"If you decide to speak, use the exec tool with `moltnet send --target ... --text ...` and choose an explicit target.",
		"",
		fmt.Sprintf(`{"kind":"bootstrap","source":"moltnet","network_id":%q,"conversation":%q,"target":{"kind":%q,"room_id":%q}}`,
			config.Moltnet.NetworkID,
			conversationContextIDForTarget(config.Moltnet.NetworkID, target),
			target.Kind,
			target.RoomID,
		),
	}, "\n")
}
