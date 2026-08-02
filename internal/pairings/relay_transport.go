package pairings

import (
	"context"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// relayTransport reserves the transport seam for future relay networking.
type relayTransport struct{}

func (r *relayTransport) FetchNetwork(context.Context, protocol.Pairing) (protocol.Network, error) {
	return protocol.Network{}, fmt.Errorf("relay transport FetchNetwork: not implemented")
}

func (r *relayTransport) FetchRooms(context.Context, protocol.Pairing) ([]protocol.Room, error) {
	return nil, fmt.Errorf("relay transport FetchRooms: not implemented")
}

func (r *relayTransport) FetchAgents(context.Context, protocol.Pairing) ([]protocol.AgentSummary, error) {
	return nil, fmt.Errorf("relay transport FetchAgents: not implemented")
}

func (r *relayTransport) RelayMessage(
	context.Context,
	protocol.Pairing,
	protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	return protocol.MessageAccepted{}, fmt.Errorf("relay transport RelayMessage: not implemented")
}

var _ pairingTransport = (*relayTransport)(nil)
