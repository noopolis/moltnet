package main

import "fmt"

// defaultMoltnetConfig renders the canonical server config `moltnet init`
// writes. networkID and networkName are rendered through %q (Go's
// double-quoted string escaping, which is also valid YAML double-quoted
// scalar syntax) so an operator-supplied --name containing YAML-special
// characters (":", "#", quotes, ...) can never corrupt the document.
//
// PLAN.md phase 6a: new networks bind loopback-only by default
// (listen_addr: 127.0.0.1:8787, not the old ":8787" which binds every
// interface) and open agent self-registration (auth.mode: open, which
// forces auth.agent_registration: open and auth.public_read: true —
// internal/app/config_load.go's mergeFileConfig always re-derives
// agent_registration from an "open" mode, so the explicit line below is
// documentation, not load-bearing). A local agent can then self-register
// via POST /v1/agents/register or the IDENTIFY frame and receive its own
// scoped magt_v1_ token (observe+write+attach, that agent only) instead of ever
// touching an operator credential — see PLAN.md's phase 6a field report.
// Widening listen_addr beyond loopback while agent_registration stays open
// is a deliberate, warned-about operator choice (see
// internal/app/bind_warning.go and `moltnet validate`), not something this
// default does on its own.
func defaultMoltnetConfig(networkID, networkName string) string {
	return fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: "127.0.0.1:8787"
  human_ingress: true
  debug_events: false

auth:
  mode: open
  agent_registration: open

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
//
// auth.agent_registration is deliberately left unset here (defaulting to
// "disabled", internal/app/config_load.go's defaultConfig), exactly
// matching pre-phase-6a `--bearer` behavior. An earlier draft of phase 6a
// set it to "open" here too, reasoning that the operator token should never
// have to leave the operator — but that made `--bearer` the one strictly
// *looser* change against HEAD in the whole phase 6a review (anonymous
// POST /v1/agents/register went 401 -> 201-with-write-token on a network an
// operator explicitly locked down with tokens): --bearer is the
// token-controlled opt-in, and it stays token-controlled. Plain `init`
// (defaultMoltnetConfig, below) is the one path with open self-registration
// by design, and it is strictly tighter than the old `auth.mode: none`
// default, not looser — see its own doc comment.
func bearerMoltnetConfig(networkID, networkName, operatorToken, consoleToken string) string {
	return fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: "127.0.0.1:8787"
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
