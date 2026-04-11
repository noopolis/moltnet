package picoclaw

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const picoWriteGracePeriod = 1 * time.Second
const defaultPicoToken = "spawnfile-internal-pico"

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

type picoEnvelope struct {
	Type      string         `json:"type"`
	SessionID string         `json:"session_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func runEventLoop(
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
				observability.Logger(ctx, "bridge.picoclaw", "agent_id", config.Agent.ID, "error", err).
					Warn("picoclaw bridge bootstrap error")

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
			if !loop.ShouldHandle(config, event) {
				return nil
			}

			return sendEventDispatch(ctx, config, event)
		})
		if err == nil || ctx.Err() != nil {
			return err
		}
		attempt++

		observability.Logger(ctx, "bridge.picoclaw", "agent_id", config.Agent.ID, "error", err).
			Warn("picoclaw bridge inbound stream error")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff.Delay(attempt)):
		}
	}
}

func sendBootstrapDispatches(ctx context.Context, config bridgeconfig.Config) error {
	for _, target := range bootstrapTargets(config) {
		prompt := buildBootstrapMessage(config, target, true)
		if err := sendPicoPrompt(ctx, config, picoSessionKeyForTarget(config, target), prompt); err != nil {
			return err
		}
	}

	return nil
}

func bootstrapTargets(config bridgeconfig.Config) []protocol.Target {
	targets := make([]protocol.Target, 0, len(config.Rooms))

	for _, binding := range config.Rooms {
		if strings.TrimSpace(binding.ID) == "" ||
			!bridgeutil.ShouldReply(binding.Reply) ||
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

	prompt := buildInboundMessage(config, event, true)
	return sendPicoPrompt(ctx, config, picoSessionKey(config, event.Message), prompt)
}

func sendPicoPrompt(
	ctx context.Context,
	config bridgeconfig.Config,
	sessionID string,
	prompt string,
) error {
	if strings.TrimSpace(config.Runtime.EventsURL) == "" {
		return fmt.Errorf("picoclaw bridge requires runtime.events_url")
	}

	socketURL, err := url.Parse(config.Runtime.EventsURL)
	if err != nil {
		return fmt.Errorf("parse pico websocket url: %w", err)
	}
	query := socketURL.Query()
	query.Set("session_id", sessionID)
	if token := picoRuntimeToken(config); token != "" {
		query.Set("token", token)
	}
	socketURL.RawQuery = query.Encode()

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	if token := picoRuntimeToken(config); token != "" {
		dialer.Subprotocols = []string{"token." + token}
	}
	headers := http.Header{}
	if token := picoRuntimeToken(config); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	conn, response, err := dialer.DialContext(ctx, socketURL.String(), headers)
	if err != nil {
		if response != nil {
			return fmt.Errorf("connect pico websocket %s: %w (status %s)", socketURL.Redacted(), err, response.Status)
		}
		return fmt.Errorf("connect pico websocket %s: %w", socketURL.Redacted(), err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("set pico websocket write deadline: %w", err)
	}
	if err := conn.WriteJSON(picoEnvelope{
		Type:      "message.send",
		SessionID: sessionID,
		Payload: map[string]any{
			"content": prompt,
		},
	}); err != nil {
		return fmt.Errorf("write pico websocket message: %w", err)
	}

	timer := time.NewTimer(picoWriteGracePeriod)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil
	case <-timer.C:
		return nil
	}
}

func picoRuntimeToken(config bridgeconfig.Config) string {
	if token := strings.TrimSpace(config.Runtime.Token); token != "" {
		return token
	}
	return defaultPicoToken
}

func buildInboundMessage(config bridgeconfig.Config, event protocol.Event, includeSessionHeader bool) string {
	if event.Message == nil {
		return ""
	}

	return bridgeutil.RenderCompactInboundMessage(
		config.Moltnet.NetworkID,
		event.Message,
		includeSessionHeader,
	)
}

func buildBootstrapMessage(config bridgeconfig.Config, target protocol.Target, includeSessionHeader bool) string {
	return bridgeutil.RenderCompactBootstrapMessage(config.Moltnet.NetworkID, target, includeSessionHeader)
}

func picoSessionKey(config bridgeconfig.Config, message *protocol.Message) string {
	contextID := conversationContextID(config.Moltnet.NetworkID, message)
	return picoSessionKeyFromContext(config.Agent.ID, contextID)
}

func picoSessionKeyForTarget(config bridgeconfig.Config, target protocol.Target) string {
	contextID := conversationContextIDForTarget(config.Moltnet.NetworkID, target)
	return picoSessionKeyFromContext(config.Agent.ID, contextID)
}

func picoSessionKeyFromContext(agentID string, contextID string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(contextID), "moltnet:")
	if trimmed == "" {
		trimmed = "main"
	}
	return fmt.Sprintf("agent:%s:%s", strings.TrimSpace(agentID), trimmed)
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
