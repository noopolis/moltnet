package tinyclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type apiClient struct {
	httpClient  *http.Client
	inboundURL  string
	outboundURL string
	ackURL      string
}

type tinyclawMessageRequest struct {
	Message   string `json:"message"`
	Agent     string `json:"agent,omitempty"`
	Sender    string `json:"sender,omitempty"`
	SenderID  string `json:"senderId,omitempty"`
	Channel   string `json:"channel,omitempty"`
	MessageID string `json:"messageId,omitempty"`
}

type tinyclawPendingResponse struct {
	ID       int               `json:"id"`
	Sender   string            `json:"sender"`
	SenderID string            `json:"senderId"`
	Message  string            `json:"message"`
	Files    []string          `json:"files,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

func newAPIClient(config bridgeconfig.Config) *apiClient {
	return &apiClient{
		httpClient:  &http.Client{},
		inboundURL:  config.Runtime.InboundURL,
		outboundURL: config.Runtime.OutboundURL,
		ackURL:      strings.TrimRight(config.Runtime.AckURL, "/"),
	}
}

func (c *apiClient) postMessage(ctx context.Context, request tinyclawMessageRequest) error {
	return c.postJSON(ctx, c.inboundURL, request, nil)
}

func (c *apiClient) pendingResponses(ctx context.Context) ([]tinyclawPendingResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.outboundURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build tinyclaw pending request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request tinyclaw pending responses: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("tinyclaw pending responses returned %s", response.Status)
	}

	var payload []tinyclawPendingResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode tinyclaw pending responses: %w", err)
	}

	return payload, nil
}

func (c *apiClient) ackResponse(ctx context.Context, id int) error {
	url := c.ackURL + "/" + strconv.Itoa(id) + "/ack"
	return c.postJSON(ctx, url, map[string]any{}, nil)
}

func (c *apiClient) postJSON(ctx context.Context, url string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode tinyclaw request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build tinyclaw request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request tinyclaw: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("tinyclaw request returned %s", response.Status)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode tinyclaw response: %w", err)
	}

	return nil
}
