package pairings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/noopolis/moltnet/internal/pairings/relay"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type relayTransport struct {
	mu      sync.RWMutex
	clients map[string]*relay.Client
}

func newRelayTransport() *relayTransport {
	return &relayTransport{clients: make(map[string]*relay.Client)}
}

func (r *relayTransport) register(pairingID string, client *relay.Client) {
	pairingID = strings.TrimSpace(pairingID)
	if pairingID == "" || client == nil {
		return
	}

	r.mu.Lock()
	r.clients[pairingID] = client
	r.mu.Unlock()
}

func (r *relayTransport) FetchNetwork(ctx context.Context, pairing protocol.Pairing) (protocol.Network, error) {
	var network protocol.Network
	if err := r.callJSON(ctx, pairing, http.MethodGet, "/v1/network", nil, &network); err != nil {
		return protocol.Network{}, err
	}
	return network, nil
}

func (r *relayTransport) FetchRooms(ctx context.Context, pairing protocol.Pairing) ([]protocol.Room, error) {
	var payload struct {
		Rooms []protocol.Room `json:"rooms"`
	}
	if err := r.callJSON(ctx, pairing, http.MethodGet, "/v1/rooms", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Rooms, nil
}

func (r *relayTransport) FetchAgents(ctx context.Context, pairing protocol.Pairing) ([]protocol.AgentSummary, error) {
	var payload struct {
		Agents []protocol.AgentSummary `json:"agents"`
	}
	if err := r.callJSON(ctx, pairing, http.MethodGet, "/v1/agents", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Agents, nil
}

func (r *relayTransport) RelayMessage(
	ctx context.Context,
	pairing protocol.Pairing,
	requestPayload protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	requestBody, err := json.Marshal(requestPayload)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("encode paired message relay: %w", err)
	}

	var accepted protocol.MessageAccepted
	if err := r.callJSON(ctx, pairing, http.MethodPost, "/v1/messages", requestBody, &accepted); err != nil {
		return protocol.MessageAccepted{}, err
	}
	return accepted, nil
}

func (r *relayTransport) callJSON(
	ctx context.Context,
	pairing protocol.Pairing,
	method string,
	path string,
	body []byte,
	target any,
) error {
	client, err := r.clientFor(pairing)
	if err != nil {
		return err
	}

	status, responseBody, err := client.Call(ctx, method, path, body)
	if err != nil {
		return fmt.Errorf("call relay pairing %q: %w", pairing.ID, err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("relay pairing %q returned HTTP status %d", pairing.ID, status)
	}
	if err := json.NewDecoder(io.LimitReader(bytes.NewReader(responseBody), maxPairingResponseBytes)).Decode(target); err != nil {
		return fmt.Errorf("decode relay pairing %q response: %w", pairing.ID, err)
	}
	return nil
}

func (r *relayTransport) clientFor(pairing protocol.Pairing) (*relay.Client, error) {
	pairingID := strings.TrimSpace(pairing.ID)
	if pairingID == "" {
		return nil, fmt.Errorf("relay pairing has no ID")
	}

	r.mu.RLock()
	client := r.clients[pairingID]
	r.mu.RUnlock()
	if client == nil {
		return nil, fmt.Errorf("relay client is not registered for pairing %q", pairingID)
	}
	return client, nil
}

var _ pairingTransport = (*relayTransport)(nil)
