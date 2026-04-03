package tinyclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const tinyclawRequestTimeout = 15 * time.Second
const maxTinyclawResponseBytes = 1 << 20

type apiClient struct {
	httpClient  *http.Client
	inboundURL  string
	outboundURL string
	ackURL      string
	token       string
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
	ID       pendingResponseID `json:"id"`
	Sender   string            `json:"sender"`
	SenderID string            `json:"senderId"`
	Message  string            `json:"message"`
	Files    []string          `json:"files,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type pendingResponseID string

func (p *pendingResponseID) UnmarshalJSON(data []byte) error {
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		return p.set(strings.TrimSpace(stringValue))
	}

	var numberValue int64
	if err := json.Unmarshal(data, &numberValue); err == nil {
		return p.set(strconv.FormatInt(numberValue, 10))
	}

	return fmt.Errorf("unsupported pending response id %s", strings.TrimSpace(string(data)))
}

func (p pendingResponseID) String() string {
	return strings.TrimSpace(string(p))
}

func (p *pendingResponseID) set(value string) error {
	trimmed := strings.TrimSpace(value)
	if err := protocol.ValidateMessageID(trimmed); err != nil {
		return fmt.Errorf("invalid pending response id: %w", err)
	}
	*p = pendingResponseID(trimmed)
	return nil
}

func newAPIClient(config bridgeconfig.Config) *apiClient {
	return &apiClient{
		httpClient:  &http.Client{Timeout: tinyclawRequestTimeout},
		inboundURL:  config.Runtime.InboundURL,
		outboundURL: config.Runtime.OutboundURL,
		ackURL:      strings.TrimRight(config.Runtime.AckURL, "/"),
		token:       config.Runtime.Token,
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
	c.authorize(request)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request tinyclaw pending responses: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("tinyclaw pending responses returned %s", response.Status)
	}

	var payload []tinyclawPendingResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, maxTinyclawResponseBytes)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode tinyclaw pending responses: %w", err)
	}

	return payload, nil
}

func (c *apiClient) ackResponse(ctx context.Context, id pendingResponseID) error {
	if err := protocol.ValidateMessageID(id.String()); err != nil {
		return fmt.Errorf("invalid pending response id: %w", err)
	}
	url := c.ackURL + "/" + id.String() + "/ack"
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
	c.authorize(request)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request tinyclaw %s: %w", observability.RedactURL(url), err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("tinyclaw request returned %s", response.Status)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(io.LimitReader(response.Body, maxTinyclawResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("decode tinyclaw response: %w", err)
	}

	return nil
}

func (c *apiClient) authorize(request *http.Request) {
	if token := strings.TrimSpace(c.token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}
