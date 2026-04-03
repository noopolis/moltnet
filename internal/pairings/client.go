package pairings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const defaultPairingRequestTimeout = 10 * time.Second
const maxPairingResponseBytes = 1 << 20

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: defaultPairingRequestTimeout},
	}
}

func (c *Client) FetchNetwork(ctx context.Context, pairing protocol.Pairing) (protocol.Network, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(pairing, "/v1/network"), nil)
	if err != nil {
		return protocol.Network{}, fmt.Errorf("build remote network request: %w", err)
	}
	authorizeRequest(request, pairing)

	var network protocol.Network
	if err := c.doJSON(request, &network); err != nil {
		return protocol.Network{}, err
	}

	return network, nil
}

func (c *Client) FetchRooms(ctx context.Context, pairing protocol.Pairing) ([]protocol.Room, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(pairing, "/v1/rooms"), nil)
	if err != nil {
		return nil, fmt.Errorf("build remote rooms request: %w", err)
	}
	authorizeRequest(request, pairing)

	var payload struct {
		Rooms []protocol.Room `json:"rooms"`
	}
	if err := c.doJSON(request, &payload); err != nil {
		return nil, err
	}

	return payload.Rooms, nil
}

func (c *Client) FetchAgents(ctx context.Context, pairing protocol.Pairing) ([]protocol.AgentSummary, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(pairing, "/v1/agents"), nil)
	if err != nil {
		return nil, fmt.Errorf("build remote agents request: %w", err)
	}
	authorizeRequest(request, pairing)

	var payload struct {
		Agents []protocol.AgentSummary `json:"agents"`
	}
	if err := c.doJSON(request, &payload); err != nil {
		return nil, err
	}

	return payload.Agents, nil
}

func (c *Client) RelayMessage(
	ctx context.Context,
	pairing protocol.Pairing,
	requestPayload protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	requestBody, err := json.Marshal(requestPayload)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("encode paired message relay: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(pairing, "/v1/messages"), bytes.NewReader(requestBody))
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("build paired message relay request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	authorizeRequest(request, pairing)

	var accepted protocol.MessageAccepted
	if err := c.doJSON(request, &accepted); err != nil {
		return protocol.MessageAccepted{}, err
	}

	return accepted, nil
}

func (c *Client) doJSON(request *http.Request, target any) error {
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("request paired network %s: %w", observability.RedactURL(request.URL.String()), err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("paired network returned %s", response.Status)
	}

	if err := json.NewDecoder(io.LimitReader(response.Body, maxPairingResponseBytes)).Decode(target); err != nil {
		return fmt.Errorf("decode paired network response: %w", err)
	}

	return nil
}

func endpoint(pairing protocol.Pairing, path string) string {
	return strings.TrimRight(pairing.RemoteBaseURL, "/") + path
}

func authorizeRequest(request *http.Request, pairing protocol.Pairing) {
	if token := strings.TrimSpace(pairing.Token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}
