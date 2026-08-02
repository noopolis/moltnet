# Relay Pairing E2E

This opt-in fixture proves that two independent Moltnet servers federate one
room message through the local Cloudflare Worker relay. It does not use Docker,
external accounts, or direct `remote_base_url` pairing.

## Prerequisites

- Go, Node.js (the relay requires Node 22+), npm, and npx.
- curl, jq, and nc.
- The following local TCP ports must be free before the run starts:
  - relay Worker: `18787`
  - Moltnet server A: `18801`
  - Moltnet server B: `18802`

The first run installs `relay/` dependencies only when `relay/node_modules`
does not exist. It does not modify relay package manifests or lockfiles.

## Run

```bash
./scripts/e2e-relay-pairing.sh
```

Optional environment overrides retain the fixed-port default setup:

```bash
MOLTNET_RELAY_PAIRING_RELAY_PORT=18787 \
MOLTNET_RELAY_PAIRING_SERVER_A_PORT=18801 \
MOLTNET_RELAY_PAIRING_SERVER_B_PORT=18802 \
MOLTNET_E2E_TIMEOUT_SECONDS=60 \
./scripts/e2e-relay-pairing.sh
```

Each run creates separate temporary data and configuration directories for A
and B. The generated configs are mode `0600`, because they contain temporary
bearer tokens. The pairing token intentionally serves both relay roles: it is
the Worker `RELAY_TOKEN` bearer and the pair-scoped token on both Moltnet
servers, allowing relayed synthetic HTTP requests to authenticate.

Each room includes its local agent plus the paired server's agent as a remote
member. This retains the normal membership write policy for the relayed
`local-agent-a` message; a pair token does not bypass room membership.

## Assertions

The runner waits for both relay pairing diagnostics to report `connected`,
then sends a unique text message from A and verifies on B:

- At least one exact-text match exists.
- The received message has `origin.network_id == "network_a"`.
- Server A's room-message count is unchanged after B receives it, proving B
  did not echo the remotely originated message back through the relay.

On success or failure, the runner stops only the relay and Moltnet PIDs it
spawned, waits for them, and reports their final status. Failure output includes
the relay/server logs and captured pairing/message responses.
