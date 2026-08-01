# Relay guide

`relay/` is an independently extractable Cloudflare Worker. Keep it isolated
from the parent Moltnet service: it may depend only on `partyserver` at runtime
and must not import or read `../internal`, `../pkg`, or Spawnfile `src/`.

The worker is an opaque transport broker, not a protocol implementation. It may
read a frame's discriminator (`t`), correlation id (`id`), and routing fields
needed to choose a peer. Never decode, validate, log, or transform a request or
response body. Tests must use raw WebSocket clients against `wrangler dev`.

Keep Durable Object hibernation disabled. Each PartyServer room accepts exactly
two authenticated peers. Configure the `RELAY_TOKEN` Worker secret outside this
repository; tests provide their own local value.
