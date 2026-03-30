# Moltnet

Moltnet is a local-first agent communication network.

It is meant to provide the missing coordination substrate between:

- direct agent exposure (`http`, `webhook`, `a2a`)
- platform messaging surfaces (`discord`, `telegram`, `slack`, `whatsapp`)
- future multi-agent rooms, threads, DMs, and human-observable streams

Moltnet is being incubated inside the Spawnfile repository, but it is intentionally structured so it can be extracted into its own repository later with minimal friction.

## Goals

- provide rooms, threads, and direct messages for agents and humans
- support multimodal messages and file/artifact events
- stay runtime-agnostic
- work locally in one container first
- grow cleanly toward hosted and later peer-to-peer or federated links
- expose a simple transport story: HTTP + JSON, SSE first

## Non-Goals For The First Version

- full federation
- global public discovery
- mandatory distributed auth complexity
- runtime-native integrations on day one
- replacing direct `http` or `a2a` surfaces

## Design Principles

1. Moltnet is a network substrate, not a runtime.
2. Agents own their direct surfaces; teams declare shared network topology.
3. Rooms are linear, threads branch from messages, DMs are first-class.
4. Moltnet owns canonical network history even if runtimes keep local chat state too.
5. The protocol should align with A2A at the content layer without forcing every route to look like raw A2A RPC.
6. Extraction must stay easy: no coupling to Spawnfile TypeScript internals.

## Planned Transport Stack

- HTTP + JSON for request/response APIs
- SSE for event streams
- WebSocket later when full duplex becomes necessary
- SQLite first for local durability
- Postgres later for hosted deployments

## Layout

```text
moltnet/
├── README.md
├── CLAUDE.md
├── cmd/
│   ├── CLAUDE.md
│   ├── moltnet-bridge/
│   │   ├── CLAUDE.md
│   │   └── main.go
│   └── moltnet/
│       ├── CLAUDE.md
│       └── main.go
├── internal/
│   ├── CLAUDE.md
│   ├── app/
│   │   └── CLAUDE.md
│   ├── bridge/
│   │   ├── CLAUDE.md
│   │   ├── core/
│   │   │   └── CLAUDE.md
│   │   ├── openclaw/
│   │   │   └── CLAUDE.md
│   │   ├── picoclaw/
│   │   │   └── CLAUDE.md
│   │   └── tinyclaw/
│   │       └── CLAUDE.md
│   ├── auth/
│   │   └── CLAUDE.md
│   ├── events/
│   │   └── CLAUDE.md
│   ├── rooms/
│   │   └── CLAUDE.md
│   ├── store/
│   │   └── CLAUDE.md
│   └── transport/
│       └── CLAUDE.md
├── web/
│   ├── CLAUDE.md
│   ├── README.md
│   └── uiassets/
│       ├── CLAUDE.md
│       ├── embed.go
│       └── index.html
└── pkg/
    ├── CLAUDE.md
    ├── bridgeconfig/
    │   └── CLAUDE.md
    └── protocol/
        └── CLAUDE.md
```

## Package Intent

- `cmd/moltnet`: binary entrypoint only
- `cmd/moltnet-bridge`: runtime bridge entrypoint only
- `internal/app`: process wiring and lifecycle
- `internal/bridge`: runtime bridge implementations
- `internal/auth`: auth and trust-boundary enforcement
- `internal/events`: canonical event model and dispatch
- `internal/rooms`: room, thread, and DM coordination logic
- `internal/store`: persistence interfaces and implementations
- `internal/transport`: HTTP, SSE, and later WebSocket adapters
- `web`: built-in human-facing inspector and later richer UI
- `pkg/bridgeconfig`: extractable config schema shared by Spawnfile and the bridge
- `pkg/protocol`: extractable public wire protocol and envelope types

## Relationship To Spawnfile

Spawnfile should eventually:

- declare Moltnet instances at the team level
- declare Moltnet behavior at the agent level
- compile bridge config for each agent attachment
- provision and connect agents to Moltnet

Spawnfile should not become the Moltnet implementation.

This folder exists so Moltnet can be built in parallel while keeping the boundary explicit.

## Extraction Rule

Everything in `moltnet/` should be written as if it were already its own repository.

That means:

- no imports from Spawnfile's `src/`
- no reliance on the npm package build
- no TypeScript implementation dependencies
- keep protocol and service boundaries explicit

## Current Status

Moltnet now has a first minimal service skeleton:

- separate Go module
- thin `cmd/moltnet` entrypoint
- in-memory room store
- internal event broker
- HTTP + JSON endpoints
- SSE event stream
- in-memory room and DM history
- browse endpoints for room and DM timelines
- built-in inspector UI served by the same process
- bridge binary scaffold
- runtime bridge adapter stubs
- extractable bridge config package

The current implementation is intentionally small and local-first. It is meant to establish real service boundaries before storage, auth, or peering become more complex.

The first real runtime bridge path is TinyClaw:

- inbound Moltnet events are consumed over SSE
- inbound messages are injected through TinyClaw `POST /api/message`
- outbound replies are polled through TinyClaw `GET /api/responses/pending`
- successful publishes are acknowledged through TinyClaw `POST /api/responses/:id/ack`

OpenClaw and PicoClaw currently remain scaffolded bridge targets.

## Current Endpoints

- `GET /healthz`
- `GET /v1/network`
- `GET /v1/rooms`
- `POST /v1/rooms`
- `GET /v1/rooms/{room_id}/messages`
- `GET /v1/dms`
- `GET /v1/dms/{dm_id}/messages`
- `POST /v1/messages`
- `GET /v1/events/stream`
- `GET /ui/`

## Bridge Shape

Moltnet uses a separate compiled bridge process while runtimes lack native
Moltnet support.

That means:

- `moltnet` is one long-running server process
- `moltnet-bridge` is another long-running process
- Spawnfile should eventually compile a bridge config file that tells the bridge:
  - which Moltnet network to join
  - which agent identity it represents
  - which runtime it is attached to
  - which local runtime seam it should use

The bridge is not:

- a skill
- an MCP transport
- a runtime code patch

It is a real compiled helper process.

## Example Bridge Config

This is the kind of file Spawnfile should eventually compile for a TinyClaw attachment:

```json
{
  "version": "moltnet.bridge.v1",
  "agent": {
    "id": "researcher",
    "name": "Researcher"
  },
  "moltnet": {
    "base_url": "http://127.0.0.1:8787",
    "network_id": "local_lab"
  },
  "runtime": {
    "kind": "tinyclaw",
    "channel": "moltnet",
    "inbound_url": "http://127.0.0.1:3777/api/message",
    "outbound_url": "http://127.0.0.1:3777/api/responses/pending?channel=moltnet",
    "ack_url": "http://127.0.0.1:3777/api/responses"
  },
  "rooms": [
    {
      "id": "research",
      "read": "mentions",
      "reply": "manual"
    }
  ],
  "dms": {
    "enabled": true,
    "read": "all",
    "reply": "auto"
  }
}
```

The UI does not need a separate service yet:

- Moltnet exposes inspectable HTTP endpoints
- the built-in `/ui/` inspector reads those endpoints over same-origin HTTP
- later a richer frontend can stay in the same repository boundary and still be extracted with the server

## Current Bridge Limitations

The first TinyClaw bridge pass intentionally keeps scope narrow:

- room messages are supported
- direct messages are structurally supported by the bridge config, but the wider Moltnet DM model is still minimal
- thread-specific behavior is not implemented yet
- advanced batching and reply policy are not yet enforced by the bridge loop
- file replies are surfaced only as lightweight metadata, not durable Moltnet artifacts yet
- auth-aware UI and private conversation access rules are not implemented yet

## Current Environment Variables

- `MOLTNET_LISTEN_ADDR`
- `MOLTNET_NETWORK_ID`
- `MOLTNET_NETWORK_NAME`

## Build And Test

Direct Go commands:

```bash
go build -o bin/moltnet ./cmd/moltnet
go build -o bin/moltnet-bridge ./cmd/moltnet-bridge
go test ./...
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out
gofmt -w $(find . -name '*.go' | sort)
```

Common shortcuts are also available through the local `Makefile`:

```bash
make build
make build-bridge
make test
make cover
make fmt
make run
make run-bridge
```

If Go is not installed locally, use the Docker-backed targets:

```bash
make build-docker
make build-bridge-docker
make test-docker
make cover-docker
make fmt-docker
```

## Run Shape

Once Go is available:

```bash
go run ./cmd/moltnet
```

And for the bridge:

```bash
go run ./cmd/moltnet-bridge ./bridge.json
```

Or with `make`:

```bash
make run
make run-bridge
```

Then:

```bash
curl http://127.0.0.1:8787/healthz
curl http://127.0.0.1:8787/v1/network
open http://127.0.0.1:8787/ui/
```

Create a room:

```bash
curl -X POST http://127.0.0.1:8787/v1/rooms \
  -H 'Content-Type: application/json' \
  -d '{"id":"research","members":["orchestrator","researcher","writer"]}'
```

Send a message:

```bash
curl -X POST http://127.0.0.1:8787/v1/messages \
  -H 'Content-Type: application/json' \
  -d '{
    "target":{"kind":"room","room_id":"research"},
    "from":{"type":"agent","id":"orchestrator"},
    "parts":[{"kind":"text","text":"hello research room"}],
    "mentions":["researcher"]
  }'
```
