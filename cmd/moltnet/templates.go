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

// bearerMoltnetConfig is defaultMoltnetConfig plus `--init --bearer`'s two
// generated tokens: auth.mode: bearer with an "operator" auth.tokens[]
// entry scoped for observing, writing, admin commands, and pairing, plus a
// "console" entry scoped to exactly [observe] — the read-only credential
// `moltnet console` requires before it will ever put a token in the
// browser's URL bar (consoleObserveToken, console.go). Minting both up
// front means the canonical `init --bearer` -> `console` flow never lands
// an operator on a raw 401: the console token already exists by the time
// `moltnet console` runs.
func bearerMoltnetConfig(networkID, networkName, operatorToken, consoleToken string) string {
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
    - id: console
      value: %q
      scopes: [observe]

rooms: []
pairings: []
`, networkID, networkName, operatorToken, consoleToken)
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
