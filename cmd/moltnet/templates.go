package main

import "fmt"

// defaultMoltnetConfig renders the canonical server config `moltnet init`
// writes. networkID and networkName are rendered through %q (Go's
// double-quoted string escaping, which is also valid YAML double-quoted
// scalar syntax) so an operator-supplied --name containing YAML-special
// characters (":", "#", quotes, ...) can never corrupt the document.
func defaultMoltnetConfig(networkID, networkName string) string {
	return fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: ":8787"
  human_ingress: true
  debug_events: false

storage:
  kind: sqlite
  sqlite:
    path: .moltnet/moltnet.db

rooms: []
pairings: []
`, networkID, networkName)
}

// bearerMoltnetConfig is defaultMoltnetConfig plus `--init --bearer`'s
// generated operator token: auth.mode: bearer with one auth.tokens[] entry
// scoped for observing, writing, admin commands, and pairing.
func bearerMoltnetConfig(networkID, networkName, operatorToken string) string {
	return fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: ":8787"
  human_ingress: true
  debug_events: false

storage:
  kind: sqlite
  sqlite:
    path: .moltnet/moltnet.db

auth:
  mode: bearer
  tokens:
    - id: operator
      value: %q
      scopes: [observe, write, admin, pair]

rooms: []
pairings: []
`, networkID, networkName, operatorToken)
}

// defaultMoltnetNodeConfig renders the canonical MoltnetNode config
// `moltnet init` writes, pointed at the network id it just created the
// server config for.
func defaultMoltnetNodeConfig(networkID string) string {
	return fmt.Sprintf(`version: moltnet.node.v1

moltnet:
  base_url: http://127.0.0.1:8787
  network_id: %q

attachments: []
`, networkID)
}
