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

func RunControlLoop(ctx context.Context, config bridgeconfig.Config) error {
	client := NewMoltnetClient(config)
	controlClient := &http.Client{Timeout: controlRequestTimeout}
	backoff := bridgeutil.NewBackoff(bridgeutil.DefaultReconnectBaseDelay, bridgeutil.DefaultReconnectMaxDelay)
	attempt := 0

	for {
		if ctx.Err() != nil {
			return nil
		}

		err := client.StreamEvents(ctx, config, func(event protocol.Event) error {
			if !ShouldHandle(config, event) {
				return nil
			}

			response, err := sendControlMessage(ctx, controlClient, config, event)
			if err != nil {
				return err
			}

			if strings.TrimSpace(response.Message) == "" {
				return nil
			}

			_, err = client.SendMessage(ctx, protocol.SendMessageRequest{
				From: protocol.Actor{
					Type: "agent",
					ID:   config.Agent.ID,
					Name: bridgeutil.DisplayName(config.Agent),
				},
				Mentions: bridgeutil.ParseMentions(response.Message),
				Parts: []protocol.Part{
					{
						Kind: protocol.PartKindText,
						Text: response.Message,
					},
				},
				Target: event.Message.Target,
			})
			return err
		})

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

func sendControlMessage(
	ctx context.Context,
	controlClient *http.Client,
	config bridgeconfig.Config,
	event protocol.Event,
) (controlResponse, error) {
	if event.Message == nil {
		return controlResponse{}, fmt.Errorf("event has no message")
	}

	body, err := json.Marshal(controlRequest{
		ContextID: conversationContextID(config.Moltnet.NetworkID, event.Message),
		From:      bridgeutil.SenderName(event.Message.From),
		Message:   bridgeutil.RenderInboundText(event.Message),
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
