package app

import (
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestBindPairingNetworks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tokens   []authn.TokenConfig
		pairings []protocol.Pairing
		want     string
	}{
		{
			name:   "unique match binds network",
			tokens: []authn.TokenConfig{{Value: "pair-secret"}},
			pairings: []protocol.Pairing{{
				Token:           "pair-secret",
				RemoteNetworkID: "remote-b",
				RemoteBaseURL:   "https://remote-b.example.com",
			}},
			want: "remote-b",
		},
		{
			name:   "whitespace-only token difference binds network",
			tokens: []authn.TokenConfig{{Value: " pair-secret "}},
			pairings: []protocol.Pairing{{
				Token:           "pair-secret",
				RemoteNetworkID: "remote-b",
			}},
			want: "remote-b",
		},
		{
			name:     "no match leaves network empty",
			tokens:   []authn.TokenConfig{{Value: "pair-secret"}},
			pairings: []protocol.Pairing{{Token: "another-secret", RemoteNetworkID: "remote-b"}},
		},
		{
			name:   "ambiguous match leaves network empty",
			tokens: []authn.TokenConfig{{Value: "shared-secret"}},
			pairings: []protocol.Pairing{
				{Token: "shared-secret", RemoteNetworkID: "remote-b"},
				{Token: "shared-secret", RemoteNetworkID: "remote-c"},
			},
		},
		{
			name:   "explicit network is never overwritten",
			tokens: []authn.TokenConfig{{Value: "pair-secret", Network: "operator-declared"}},
			pairings: []protocol.Pairing{{
				Token:           "pair-secret",
				RemoteNetworkID: "remote-b",
			}},
			want: "operator-declared",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bound := bindPairingNetworks(test.tokens, test.pairings)
			if got := bound[0].Network; got != test.want {
				t.Fatalf("bound network = %q, want %q", got, test.want)
			}
		})
	}
}
