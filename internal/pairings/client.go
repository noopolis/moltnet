package pairings

import (
	"context"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type pairingTransport interface {
	FetchNetwork(ctx context.Context, pairing protocol.Pairing) (protocol.Network, error)
	FetchRooms(ctx context.Context, pairing protocol.Pairing) ([]protocol.Room, error)
	FetchAgents(ctx context.Context, pairing protocol.Pairing) ([]protocol.AgentSummary, error)
	RelayMessage(ctx context.Context, pairing protocol.Pairing, request protocol.SendMessageRequest) (protocol.MessageAccepted, error)
}

type Client struct {
	http  *httpTransport
	relay *relayTransport
}

func NewClient() *Client {
	return &Client{
		http:  newHTTPTransport(),
		relay: &relayTransport{},
	}
}

func (c *Client) transportFor(pairing protocol.Pairing) pairingTransport {
	if pairing.Relay != nil {
		return c.relay
	}
	return c.http
}

func (c *Client) FetchNetwork(ctx context.Context, pairing protocol.Pairing) (protocol.Network, error) {
	return c.transportFor(pairing).FetchNetwork(ctx, pairing)
}

func (c *Client) FetchRooms(ctx context.Context, pairing protocol.Pairing) ([]protocol.Room, error) {
	return c.transportFor(pairing).FetchRooms(ctx, pairing)
}

func (c *Client) FetchAgents(ctx context.Context, pairing protocol.Pairing) ([]protocol.AgentSummary, error) {
	return c.transportFor(pairing).FetchAgents(ctx, pairing)
}

func (c *Client) RelayMessage(
	ctx context.Context,
	pairing protocol.Pairing,
	requestPayload protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	return c.transportFor(pairing).RelayMessage(ctx, pairing, requestPayload)
}
