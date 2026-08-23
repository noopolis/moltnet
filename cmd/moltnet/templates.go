package main

import (
	"fmt"
	"strings"
)

// defaultInitListenAddr is the loopback-only bind address `moltnet init`
// writes when --listen is not given (defaultMoltnetConfig/bearerMoltnetConfig's
// own historical hardcoded literal, PLAN.md phase 6a). Named as a constant
// now that --listen (init_listen.go) needs the same value to fall back to,
// so the two can never drift apart.
const defaultInitListenAddr = "127.0.0.1:8787"

// starterRoomID is the one room plain `init` authors when --room is not
// given -- unchanged from PLAN.md phase 7A.2's "general".
const starterRoomID = "general"

// defaultMoltnetConfig renders the canonical server config `moltnet init`
// writes. networkID and networkName are rendered through %q (Go's
// double-quoted string escaping, which is also valid YAML double-quoted
// scalar syntax) so an operator-supplied --name containing YAML-special
// characters (":", "#", quotes, ...) can never corrupt the document.
// operatorToken is a freshly generated secret (the caller uses the same
// app.GenerateRandomToken(256) --bearer does) — this function only ever
// renders it, it never generates one itself, matching bearerMoltnetConfig's
// own division of labor below.
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
//
// PLAN.md phase 7.0: plain `init` also mints exactly one operator token —
// scoped [observe, write, admin], deliberately without "pair" (a
// network-unbound pair-scoped credential collides with the pairing gates in
// internal/rooms/federation_access.go; nothing CLI-side needs it) — so the
// network's own owner has a key to administer it. auth.mode stays "open":
// open mode is already a supported, tested policy with tokens present
// (internal/auth/open_test.go), and the token rides alongside it rather
// than switching the network to bearer, which would silently close local
// self-registration. The token is never printed by this or any caller.
//
// PLAN.md phase 7A.2: plain `init` also writes one starter room, "general",
// write_policy: registered_agents (so a self-registering agent can speak
// immediately, no membership grant needed — access_policy.go's
// registeredAgentActorCanWrite), federation: none, and no credential. The
// explicit "none" federation is not decorative: validateRoomFederationStances
// (internal/app/config_file.go) errors on a nil stance once pairings[] is
// non-empty, and that validation runs on every config load — including the
// reload writePairingWithRollback (pair.go) performs after `pair invite`,
// which rolls the pairing back when the reload fails. A starter room
// written without a stance would work fine right up until the operator's
// first `pair invite`, which would then fail and silently undo itself. A
// credential is deliberately withheld too: a room credential is what gates
// POST /join (join.go), so giving the starter room one would make it
// unjoinable-by-design for exactly the fresh agents it exists to serve,
// while write_policy: registered_agents already grants write with no
// membership check at all.
//
// visibility: public is not in PLAN.md's literal field list for this room,
// but is required for that same "/install.md names it" promise to actually
// hold, and is added here as a documented correction rather than a silent
// deviation. Read access and write_policy are two fully independent axes
// (internal/rooms/access_policy.go's canReadRoom never consults
// write_policy); a self-registered agent token is agent-restricted
// (auth.HasAgentRestriction, since NewAgentTokenClaims always scopes it to
// exactly the one agent id that registered), so canReadRoom's
// agent-restricted branch requires either visibility: public or membership
// — write_policy: registered_agents grants none of that. Left at the
// unset ("private") default, GET /install.md's own room listing
// (internal/transport/discovery.go, which lists only canReadRoom-readable
// rooms) would never mention "general" to the anonymous visitor it exists
// to greet, and a freshly self-registered agent could write into the room
// but not read back its own history or anyone else's. public_read being
// forced true under auth.mode: open (this file's own top doc comment) is
// necessary for that but not sufficient without this.
func defaultMoltnetConfig(networkID, networkName, operatorToken string) string {
	return defaultMoltnetConfigWithOptions(networkID, networkName, operatorToken, defaultInitListenAddr, nil)
}

// defaultMoltnetConfigWithOptions is defaultMoltnetConfig extended with the
// two fields `moltnet init` gained flags for (PLAN.md's setup-wizard Unit
// 1): listenAddr (--listen; the pre-existing callers above all pass
// defaultInitListenAddr, unchanged from before this split) and roomIDs
// (--room, repeatable). defaultMoltnetConfig keeps its original 3-arg shape
// so none of its many existing call sites (tests included) need to know
// about either flag; runInit itself calls this function directly once it
// has resolved both.
//
// roomIDs empty or nil defaults to []string{starterRoomID} ("general"),
// exactly matching defaultMoltnetConfig's fixed behavior -- see
// renderStarterRooms.
func defaultMoltnetConfigWithOptions(networkID, networkName, operatorToken, listenAddr string, roomIDs []string) string {
	return fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: %q
  human_ingress: true
  debug_events: false

auth:
  mode: open
  agent_registration: open
  tokens:
    - id: operator
      value: %q
      scopes: [observe, write, admin]

storage:
  kind: sqlite
  sqlite:
    path: .moltnet/moltnet.db

%spairings: []
`, networkID, networkName, listenAddr, operatorToken, renderStarterRooms(roomIDs))
}

// renderStarterRooms renders the `rooms:` block plain `init` writes for
// roomIDs, one entry per id, each shaped exactly like the historical
// "general" starter room (PLAN.md phase 7A.2's defaultMoltnetConfig doc
// comment above): visibility: public, write_policy: registered_agents,
// federation: none. This is deliberately not room create's own defaults
// (visibility: private, write_policy: members, room_create.go) -- a room
// with that shape is invisible and unwritable to a self-registering agent
// (internal/rooms/access_policy.go), which is every agent the open posture
// `init --room` exclusively authors for produces. An empty/nil roomIDs
// falls back to []string{starterRoomID}, so every caller (including
// defaultMoltnetConfig's zero-flag path) always gets at least "general".
//
// Room ids are rendered unquoted, matching the original hand-written
// "id: general" literal byte-for-byte when roomIDs is exactly
// []string{"general"}: every id reaching here has already passed
// validateCanonicalRoomID's `^[a-z0-9][a-z0-9._-]*$` grammar
// (init_rooms.go), which contains no YAML-special character, so unlike
// network id/name (operator-supplied free text, rendered via %q) an
// unquoted plain scalar is always safe here.
func renderStarterRooms(roomIDs []string) string {
	if len(roomIDs) == 0 {
		roomIDs = []string{starterRoomID}
	}
	var block strings.Builder
	block.WriteString("rooms:\n")
	for _, id := range roomIDs {
		fmt.Fprintf(&block, "  - id: %s\n    visibility: public\n    write_policy: registered_agents\n    federation: none\n", id)
	}
	return block.String()
}

// bearerMoltnetConfig is defaultMoltnetConfig plus `--init --bearer`'s two
// generated tokens: auth.mode: bearer with an "operator" auth.tokens[]
// entry scoped [observe, write, admin] -- deliberately without "pair" (see
// internal/rooms/federation_access.go's isOperatorClaims: a claim carrying
// both admin and write is treated as an operator credential regardless of
// whether it also carries pair, but minting without pair here keeps freshly
// generated operator tokens out of that ambiguity altogether; F1 found that
// minting operator tokens *with* pair locked every --bearer operator out of
// their own network's writes and room listing) -- plus a "console" entry
// scoped to exactly [observe] — the read-only credential
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
//
// listenAddr threads `moltnet init --listen` (init_listen.go) into the
// bearer path too -- there is no `--bearer --room` equivalent restriction
// on `--listen`, so a bearer network can be authored non-loopback exactly
// like a plain one. There is deliberately no roomIDs parameter: `--bearer
// --room` is rejected outright by runInit before either template function
// is ever reached (bearer creates no rooms today, and the open starter
// room's write_policy: registered_agents shape is unwritable by a
// bearer-enrolled static token -- see runInit's doc comment), so rooms
// stays the fixed `[]` below.
func bearerMoltnetConfig(networkID, networkName, operatorToken, consoleToken, listenAddr string) string {
	return fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: %q
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
      scopes: [observe, write, admin]
    - id: console
      value: %q
      scopes: [observe]

rooms: []
pairings: []
`, networkID, networkName, listenAddr, operatorToken, consoleToken)
}

// defaultMoltnetNodeConfig renders the canonical MoltnetNode config
// `moltnet init` writes, pointed at the network id it just created the
// server config for and at baseURL -- the dialable HTTP origin for the
// server config's own server.listen_addr.
//
// baseURL is a caller-resolved value, not a raw listen_addr: this used to
// hardcode "http://127.0.0.1:8787" regardless of what --listen chose, so
// `init --listen 127.0.0.1:8788` (the exact scenario the port-preflight in
// PLAN's setup wizard exists for -- 8787 already taken by another live
// network) pointed the freshly written MoltnetNode scaffold at whatever
// answers on 8787 instead of the network it was just written alongside.
// Callers resolve baseURL via consoleBaseURL(listenAddr) (console.go),
// which performs the exact translation this file needs too: a wildcard
// host (":8787", "0.0.0.0:8787") becomes the dialable loopback address
// "127.0.0.1", keeping the chosen port, while any other host (an explicit
// loopback IP, or a real hostname) is used verbatim. Reusing that function
// rather than duplicating its mapping keeps the two calculations from ever
// drifting apart.
//
// base_url is rendered through %q, matching every other free-text field in
// this file (network id/name, the auth token values above): consoleBaseURL
// itself only ever derives baseURL from a --listen value that has already
// passed app.ValidateListenAddr's host:port grammar, but that grammar does
// not reject a host containing "#" or embedded whitespace/newlines --
// net.SplitHostPort only cares about the trailing ":port". An unquoted %s
// here let such a value corrupt the written YAML: "127.0.0.1 #x:9911"
// silently truncated base_url's parsed value at the "#" (a YAML comment
// marker), and an embedded newline split the scalar across lines, turning
// its remainder into an unrelated sibling top-level key. %q's Go
// double-quoted escaping is a valid YAML double-quoted scalar, so both
// characters are safely escaped inside the string instead.
func defaultMoltnetNodeConfig(networkID, baseURL string) string {
	return fmt.Sprintf(`version: moltnet.node.v1

moltnet:
  base_url: %q
  network_id: %q

attachments: []
`, baseURL, networkID)
}
