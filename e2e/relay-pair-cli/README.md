# Relay Pair CLI E2E

This opt-in fixture proves the actual pairing UX from PLAN.md phase 3 works
end to end: `moltnet pair invite --room <id>` on one network plus
`moltnet pair <code>` on the other, against a local Cloudflare Worker relay
(`npx wrangler dev`), federate a shared room and deliver a message across it.
It does not hand-write any `pairings[]` or `rooms[]` YAML — that is exactly
what the two `pair` commands under test are responsible for, including
auto-creating the shared room and wiring its `federation` to allow the new
pairing (an absent/`none` federation becomes a list naming the pairing; an
existing list gets the pairing appended; an existing `all` is left alone).

## Prerequisites

- Go, Node.js (the relay requires Node 22+), npm, and npx.
- curl and jq.

Every TCP port this harness uses (the relay Worker and both Moltnet servers)
is picked at random by asking Node to bind an ephemeral port and release it,
so concurrent runs and other local services never collide with a fixed port.

The first run installs `relay/` dependencies only when `relay/node_modules`
does not exist. It does not modify relay package manifests or lockfiles.

## Run

```bash
./scripts/e2e-relay-pair-cli.sh
```

```bash
MOLTNET_E2E_TIMEOUT_SECONDS=60 ./scripts/e2e-relay-pair-cli.sh
```

Each run creates a fresh temporary directory with its own two Moltnet server
configs, storage, and log files, and removes it on exit (success or
failure). On failure, the runner dumps the relay/server logs, the two
written config files, and the captured pairing/message responses before
cleaning up.

## What it exercises

1. Builds the `moltnet` binary once.
2. Starts the relay Worker (`npx wrangler dev --local`) with a random
   `RELAY_TOKEN`.
3. Writes a minimal pre-pairing config for network A and network B: identity,
   transport, sqlite storage, and one admin-scoped operator token each. No
   `rooms:` or `pairings:` yet.
4. Runs `moltnet pair invite --room federated_room --id pair_to_b
   --relay-url ... --relay-token-env ... --config <A's config>` and captures
   the printed invite code.
5. Runs `moltnet pair <code> --config <B's config>`.
6. Sanity-checks both configs now declare the `federated_room` room (created
   by the pair commands, not this script).
7. Starts both Moltnet servers.
8. Grants the relayed remote actor (`network_a:local-agent-a`) membership in
   the shared room on B via `moltnet admin room members add`. `pair
   invite`/`pair <code>` only wire room *federation*, not room *membership*
   (an explicit open question in PLAN.md); a federated-but-memberless room
   still refuses writes under the default `members` write policy, so this
   step supplies the one piece of setup the pairing commands intentionally
   leave to the operator, exactly as `moltnet admin room members add`'s own
   documented use is.
9. Sends a unique text message into `federated_room` on A via the HTTP API.
10. Polls B's room-messages endpoint (timeout `MOLTNET_E2E_TIMEOUT_SECONDS`,
    default 60s) for that exact text, and asserts the received message's
    `origin.network_id` is `network_a`.

On success or failure, the runner stops only the relay and Moltnet PIDs it
spawned, waits for them, and reports their final status.
