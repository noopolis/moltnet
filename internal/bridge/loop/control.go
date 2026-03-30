package loop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const reconnectDelay = 2 * time.Second

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

type moltnetClient struct {
	baseURL string
	client  *http.Client
	token   string
}

func RunControlLoop(ctx context.Context, config bridgeconfig.Config) error {
	client := &moltnetClient{
		baseURL: strings.TrimRight(config.Moltnet.BaseURL, "/"),
		client:  &http.Client{},
		token:   config.Moltnet.Token,
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		err := client.streamEvents(ctx, func(event protocol.Event) error {
			if !ShouldHandle(config, event) {
				return nil
			}

			response, err := sendControlMessage(ctx, config, event)
			if err != nil {
				return err
			}

			if strings.TrimSpace(response.Message) == "" {
				return nil
			}

			_, err = client.sendMessage(ctx, protocol.SendMessageRequest{
				From: protocol.Actor{
					Type: "agent",
					ID:   config.Agent.ID,
					Name: displayName(config.Agent),
				},
				Mentions: parseMentions(response.Message),
				Parts: []protocol.Part{
					{
						Kind: "text",
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

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(reconnectDelay):
		}
	}
}

func sendControlMessage(
	ctx context.Context,
	config bridgeconfig.Config,
	event protocol.Event,
) (controlResponse, error) {
	if event.Message == nil {
		return controlResponse{}, fmt.Errorf("event has no message")
	}

	body, err := json.Marshal(controlRequest{
		ContextID: fmt.Sprintf("moltnet:%s:%s", config.Moltnet.NetworkID, event.Message.ID),
		From:      senderName(event.Message.From),
		Message:   RenderInboundText(event.Message),
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

	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return controlResponse{}, fmt.Errorf("request control url: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return controlResponse{}, fmt.Errorf("control url returned %s", response.Status)
	}

	var payload controlResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return controlResponse{}, fmt.Errorf("decode control response: %w", err)
	}

	return payload, nil
}

func (c *moltnetClient) streamEvents(ctx context.Context, handle func(event protocol.Event) error) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/events/stream", nil)
	if err != nil {
		return fmt.Errorf("build moltnet stream request: %w", err)
	}

	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("request moltnet stream: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("moltnet stream returned %s", response.Status)
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(dataLines) == 0 {
				continue
			}

			var event protocol.Event
			if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &event); err != nil {
				return fmt.Errorf("decode moltnet event: %w", err)
			}

			if err := handle(event); err != nil {
				return err
			}

			dataLines = dataLines[:0]
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read moltnet stream: %w", err)
	}

	return nil
}

func (c *moltnetClient) sendMessage(
	ctx context.Context,
	requestPayload protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	body, err := json.Marshal(requestPayload)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("encode moltnet message: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/messages",
		bytes.NewReader(body),
	)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("build moltnet message request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("request moltnet message send: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return protocol.MessageAccepted{}, fmt.Errorf("moltnet message send returned %s", response.Status)
	}

	var accepted protocol.MessageAccepted
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("decode moltnet message response: %w", err)
	}

	return accepted, nil
}
