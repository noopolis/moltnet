# Internal Guide

This folder holds non-public Moltnet implementation code.

Nothing here should be imported by external consumers once Moltnet becomes its own repository.

## Subpackages

- `app/`: process wiring and lifecycle
- `bridge/`: runtime bridge lifecycle and adapters
- `auth/`: auth and trust-boundary policy
- `events/`: event dispatch and subscriptions
- `node/`: multi-attachment supervision and runtime wiring
- `observability/`: structured logging, metrics, and request correlation
- `pairings/`: remote network discovery and relay client
- `rooms/`: room, thread, and DM coordination
- `signals/`: shared process signal-context helpers for CLI entrypoints
- `store/`: persistence interfaces and backends
- `transport/`: HTTP, SSE, and native attachment WebSocket adapters

## Rules

- Keep boundaries crisp between transport, domain logic, and persistence.
- Avoid circular package relationships.
- Put shared types in `pkg/protocol` only if they are truly public wire concepts.
