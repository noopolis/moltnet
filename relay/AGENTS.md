# Relay guide

`relay/` is an independently extractable Cloudflare Worker. Keep it isolated
from the parent Moltnet service: it may depend only on `partyserver` at runtime
and must not import or read `../internal`, `../pkg`, or Spawnfile `src/`.

The worker is an opaque transport broker, not a protocol implementation. It may
read a frame's discriminator (`t`), correlation id (`id`), and routing fields
needed to choose a peer. Never decode, validate, log, or transform a request or
response body. Tests must use raw WebSocket clients against `wrangler dev`.

Keep Durable Object hibernation enabled. An `accept()`ed socket bills duration
for its whole connected life at the 128 MiB minimum, so a non-hibernating room
costs roughly 10,800 GB-s/day while completely idle — most of a free-tier day
for a relay that has carried nothing.

Hibernation makes one rule load-bearing: **no admission-relevant state in
instance fields.** Instance fields do not survive eviction, and a room that
forgets its admitted peers drops every frame while still accepting new ones.
Admission is marked with `connection.setState({ admitted: true })`, which
persists in the socket's serialized attachment, and is read back through
`getConnections()`. Compare connections by object identity, never by `id`:
ids are client-supplied via `?_pk=` and two peers may collide on one.

Note that `wrangler dev --local` never evicts a Durable Object, so the test
suite proves the API path but cannot prove hibernation survival. Changes here
need review against the rule above plus a post-deploy check that relays a frame
after more than 30 seconds of idle.

Each PartyServer room accepts exactly two authenticated peers. Configure the
`RELAY_TOKEN` Worker secret outside this repository; tests provide their own
local value.

`embed.go` is the one deliberate exception to "no Go here": it only
`go:embed`s the committed `dist/` bundle so `internal/relaydeploy` can reach
it without Node, and imports nothing else from this directory. Delete it
before extracting `relay/` into its own repository.
