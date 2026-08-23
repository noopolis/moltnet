package client

import (
	"context"
	"net/http"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// This file holds the client methods `moltnet pair show` (PLAN.md 7B.4)
// composes: a live diagnostic fetch of one already-paired network, its rooms,
// and its agents. Split out of network.go (ListPairings, the "list my own
// peers" leg) since these three calls are about drilling into a *specific*
// pairing rather than listing all of them, and to keep both files well under
// the repo's 400-line-per-file limit (AGENTS.md).

// GetPairingNetwork calls GET /v1/pairings/{id}/network. Server-side this
// refreshes pairing diagnostics and fetches the peer's own advertised
// network document live (internal/rooms/pairings.go's PairingNetwork), so it
// errors -- HTTP 502, wrapping internal/rooms.ErrRemotePairing -- whenever
// the peer is unreachable or has never answered a pending invite. Callers
// (pair_show.go) must not surface that transport error verbatim; see
// isPairingUnreachableError.
func (c *Client) GetPairingNetwork(ctx context.Context, pairingID string) (protocol.Network, error) {
	var network protocol.Network
	if err := c.doJSON(ctx, http.MethodGet, "/v1/pairings/"+pairingID+"/network", nil, &network); err != nil {
		return protocol.Network{}, err
	}
	return network, nil
}

// GetPairingRooms calls GET /v1/pairings/{id}/rooms -- same live-fetch and
// unreachable-peer error shape as GetPairingNetwork.
func (c *Client) GetPairingRooms(ctx context.Context, pairingID string, page protocol.PageRequest) (protocol.RoomPage, error) {
	var result protocol.RoomPage
	path := "/v1/pairings/" + pairingID + "/rooms" + encodePage(page)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return protocol.RoomPage{}, err
	}
	return result, nil
}

// GetPairingAgents calls GET /v1/pairings/{id}/agents -- same live-fetch and
// unreachable-peer error shape as GetPairingNetwork.
func (c *Client) GetPairingAgents(ctx context.Context, pairingID string, page protocol.PageRequest) (protocol.AgentPage, error) {
	var result protocol.AgentPage
	path := "/v1/pairings/" + pairingID + "/agents" + encodePage(page)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return protocol.AgentPage{}, err
	}
	return result, nil
}
