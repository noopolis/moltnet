package openclaw

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
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const maxHookResponseBytes = 1 << 20

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

type hookRequest struct {
	Message        string `json:"message"`
	Name           string `json:"name"`
	AgentID        string `json:"agentId,omitempty"`
	SessionKey     string `json:"sessionKey,omitempty"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	DisableMessage bool   `json:"disableMessageTool,omitempty"`
	Deliver        bool   `json:"deliver"`
	WakeMode       string `json:"wakeMode,omitempty"`
}

type hookPrompt struct {
	Kind         string          `json:"kind,omitempty"`
	Source       string          `json:"source"`
	NetworkID    string          `json:"network_id"`
	EventID      string          `json:"event_id,omitempty"`
	MessageID    string          `json:"message_id,omitempty"`
	Conversation string          `json:"conversation"`
	Target       protocol.Target `json:"target"`
	From         protocol.Actor  `json:"from,omitempty"`
	Mentions     []string        `json:"mentions,omitempty"`
	Text         string          `json:"text,omitempty"`
	Parts        []protocol.Part `json:"parts,omitempty"`
}

func runHookLoop(
	ctx context.Context,
	config bridgeconfig.Config,
	streamer eventStreamer,
	controlClient *http.Client,
	backoff backoffPolicy,
) error {
	attempt := 0
	bootstrapped := false

	for {
		if ctx.Err() != nil {
			return nil
		}

		if !bootstrapped {
			if err := sendBootstrapHooks(ctx, controlClient, config); err != nil {
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
			return sendHookEvent(ctx, controlClient, config, event)
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

func sendBootstrapHooks(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
) error {
	for _, target := range bootstrapTargets(config) {
		prompt, err := buildBootstrapHookMessage(config, target)
		if err != nil {
			return err
		}

		if err := sendHookRequest(ctx, controlClient, config, hookRequest{
			Message:        prompt,
			Name:           "Moltnet Bootstrap",
			AgentID:        config.Agent.ID,
			SessionKey:     hookSessionKeyForTarget(config.Moltnet.NetworkID, target),
			IdempotencyKey: hookBootstrapIdempotencyKey(config, target),
			DisableMessage: true,
			Deliver:        false,
			WakeMode:       "now",
		}); err != nil {
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
			binding.Read == bridgeconfig.ReadThreadOnly {
			continue
		}
		targets = append(targets, protocol.Target{
			Kind:   protocol.TargetKindRoom,
			RoomID: binding.ID,
		})
	}

	return targets
}

func sendHookEvent(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
	event protocol.Event,
) error {
	if event.Message == nil {
		return fmt.Errorf("event has no message")
	}

	prompt, err := buildHookMessage(config, event)
	if err != nil {
		return err
	}

	return sendHookRequest(ctx, controlClient, config, hookRequest{
		Message:        prompt,
		Name:           "Moltnet",
		AgentID:        config.Agent.ID,
		SessionKey:     hookSessionKey(config, event.Message),
		IdempotencyKey: hookIdempotencyKey(config, event),
		DisableMessage: true,
		Deliver:        false,
		WakeMode:       "now",
	})
}

func sendHookRequest(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
	payload hookRequest,
) error {
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode hook request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		config.Runtime.ControlURL,
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return fmt.Errorf("build hook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(config.Runtime.Token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := controlClient.Do(request)
	if err != nil {
		return fmt.Errorf("request hook url %s: %w", request.URL.Redacted(), err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxHookResponseBytes))
		message := strings.TrimSpace(string(body))
		if message == "" {
			return fmt.Errorf("hook url returned %s", response.Status)
		}
		return fmt.Errorf("hook url returned %s: %s", response.Status, message)
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxHookResponseBytes))
	return nil
}

func buildHookMessage(config bridgeconfig.Config, event protocol.Event) (string, error) {
	if event.Message == nil {
		return "", fmt.Errorf("event has no message")
	}

	payload, err := json.MarshalIndent(hookPrompt{
		Kind:         "message",
		Source:       "moltnet",
		NetworkID:    config.Moltnet.NetworkID,
		EventID:      event.ID,
		MessageID:    event.Message.ID,
		Conversation: conversationContextID(config.Moltnet.NetworkID, event.Message),
		Target:       event.Message.Target,
		From:         event.Message.From,
		Mentions:     append([]string(nil), event.Message.Mentions...),
		Text:         bridgeutil.RenderInboundText(event.Message),
		Parts:        append([]protocol.Part(nil), event.Message.Parts...),
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode hook prompt: %w", err)
	}

	return strings.Join([]string{
		"Moltnet inbox delivery. This is not a synchronous request.",
		"Read /workspace/skills/moltnet/SKILL.md and /workspace/.moltnet/config.json for the local Moltnet contract.",
		"If you decide to speak, use the exec tool with `moltnet send --target ... --text ...` and choose an explicit target. Staying silent is allowed.",
		"",
		string(payload),
	}, "\n"), nil
}

func buildBootstrapHookMessage(config bridgeconfig.Config, target protocol.Target) (string, error) {
	payload, err := json.MarshalIndent(hookPrompt{
		Kind:         "bootstrap",
		Source:       "moltnet",
		NetworkID:    config.Moltnet.NetworkID,
		Conversation: conversationContextIDForTarget(config.Moltnet.NetworkID, target),
		Target:       target,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode bootstrap hook prompt: %w", err)
	}

	return strings.Join([]string{
		"Moltnet bootstrap delivery. This is not a synchronous request.",
		"You are attached to a Moltnet conversation and may stay silent.",
		"Read /workspace/skills/moltnet/SKILL.md and /workspace/.moltnet/config.json for the local Moltnet contract.",
		"If you decide to speak, use the exec tool with `moltnet send --target ... --text ...` and choose an explicit target.",
		"",
		string(payload),
	}, "\n"), nil
}

func hookSessionKey(config bridgeconfig.Config, message *protocol.Message) string {
	contextID := conversationContextID(config.Moltnet.NetworkID, message)
	if contextID == "" {
		return ""
	}
	if strings.HasPrefix(contextID, "hook:") {
		return contextID
	}
	return "hook:" + contextID
}

func hookSessionKeyForTarget(networkID string, target protocol.Target) string {
	contextID := conversationContextIDForTarget(networkID, target)
	if contextID == "" {
		return ""
	}
	if strings.HasPrefix(contextID, "hook:") {
		return contextID
	}
	return "hook:" + contextID
}

func hookIdempotencyKey(config bridgeconfig.Config, event protocol.Event) string {
	if strings.TrimSpace(event.ID) == "" {
		return ""
	}
	return fmt.Sprintf("moltnet:%s:%s", config.Agent.ID, strings.TrimSpace(event.ID))
}

func hookBootstrapIdempotencyKey(config bridgeconfig.Config, target protocol.Target) string {
	contextID := conversationContextIDForTarget(config.Moltnet.NetworkID, target)
	if contextID == "" {
		return ""
	}

	return fmt.Sprintf("moltnet:%s:bootstrap:%s", config.Agent.ID, contextID)
}

func conversationContextID(networkID string, message *protocol.Message) string {
	if message == nil {
		return ""
	}

	return conversationContextIDForTarget(networkID, message.Target, message.ID)
}

func conversationContextIDForTarget(networkID string, target protocol.Target, fallbackMessageID ...string) string {
	switch target.Kind {
	case protocol.TargetKindRoom:
		if target.RoomID != "" {
			return fmt.Sprintf("moltnet:%s:room:%s", networkID, target.RoomID)
		}
	case protocol.TargetKindDM:
		if target.DMID != "" {
			return fmt.Sprintf("moltnet:%s:dm:%s", networkID, target.DMID)
		}
	case protocol.TargetKindThread:
		if target.ThreadID != "" {
			return fmt.Sprintf("moltnet:%s:thread:%s", networkID, target.ThreadID)
		}
	}

	if len(fallbackMessageID) == 0 || fallbackMessageID[0] == "" {
		return ""
	}

	return fmt.Sprintf("moltnet:%s:%s", networkID, fallbackMessageID[0])
}
