package app

import (
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func bindPairingNetworks(tokens []authn.TokenConfig, pairings []protocol.Pairing) []authn.TokenConfig {
	bound := append([]authn.TokenConfig(nil), tokens...)
	for index := range bound {
		if strings.TrimSpace(bound[index].Network) != "" || strings.TrimSpace(bound[index].Value) == "" {
			continue
		}

		var network string
		matched := false
		ambiguous := false
		for _, pairing := range pairings {
			if strings.TrimSpace(pairing.Token) != strings.TrimSpace(bound[index].Value) {
				continue
			}
			candidate := strings.TrimSpace(pairing.RemoteNetworkID)
			if !matched {
				network = candidate
				matched = true
				continue
			}
			if network != candidate {
				ambiguous = true
				break
			}
		}
		if !ambiguous {
			bound[index].Network = network
		}
	}
	return bound
}
