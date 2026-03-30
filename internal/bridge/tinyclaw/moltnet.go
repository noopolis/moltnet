package tinyclaw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type moltnetClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

type moltnetEvent = protocol.Event

func newMoltnetClient(config bridgeconfig.Config) *moltnetClient {
	return &moltnetClient{
		httpClient: &http.Client{},
		baseURL:    strings.TrimRight(config.Moltnet.BaseURL, "/"),
		token:      config.Moltnet.Token,
	}
}

func (c *moltnetClient) streamEvents(ctx context.Context, handle func(event moltnetEvent) error) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/events/stream", nil)
	if err != nil {
		return fmt.Errorf("build moltnet stream request: %w", err)
	}

	c.applyAuth(request)

	response, err := c.httpClient.Do(request)
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

			event, err := decodeEventPayload(dataLines)
			if err != nil {
				return err
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

func (c *moltnetClient) sendMessage(ctx context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("encode moltnet message: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/messages",
		bytes.NewReader(payload),
	)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("build moltnet message request: %w", err)
	}

	httpRequest.Header.Set("Content-Type", "application/json")
	c.applyAuth(httpRequest)

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("request moltnet message send: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return protocol.MessageAccepted{}, fmt.Errorf("moltnet message send returned %s", response.Status)
	}

	var accepted protocol.MessageAccepted
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("decode moltnet message send: %w", err)
	}

	return accepted, nil
}

func (c *moltnetClient) applyAuth(request *http.Request) {
	if c.token == "" {
		return
	}

	request.Header.Set("Authorization", "Bearer "+c.token)
}

func decodeEventPayload(lines []string) (moltnetEvent, error) {
	var event moltnetEvent
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &event); err != nil {
		return moltnetEvent{}, fmt.Errorf("decode moltnet event: %w", err)
	}

	return event, nil
}
