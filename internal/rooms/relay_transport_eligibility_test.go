package rooms

import (
	"testing"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRelayPairingTransportEligibility(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{
		NetworkID: "net_a",
		Store:     store.NewMemoryStore(),
		Messages:  store.NewMemoryStore(),
		Broker:    events.NewBroker(),
	})
	message := protocol.Message{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
	}

	for _, test := range []struct {
		name    string
		pairing protocol.Pairing
		want    bool
	}{
		{
			name: "relay only",
			pairing: protocol.Pairing{
				ID:     "relay",
				Status: protocol.PairingStatusConnected,
				Relay:  &protocol.PairingRelay{URL: "wss://relay.example.com", Room: "research"},
			},
			want: true,
		},
		{
			name: "http only",
			pairing: protocol.Pairing{
				ID:            "http",
				Status:        protocol.PairingStatusConnected,
				RemoteBaseURL: "https://remote.example.com",
			},
			want: true,
		},
		{
			name: "no transport",
			pairing: protocol.Pairing{
				ID:     "none",
				Status: protocol.PairingStatusConnected,
			},
			want: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := service.shouldRelayToPairing(test.pairing, message); got != test.want {
				t.Fatalf("shouldRelayToPairing() = %t, want %t", got, test.want)
			}
			if got := service.shouldAttemptRelayToPairing(test.pairing, message); got != test.want {
				t.Fatalf("shouldAttemptRelayToPairing() = %t, want %t", got, test.want)
			}
		})
	}
}
