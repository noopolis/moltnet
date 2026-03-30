# Internal Guide

This folder holds non-public Moltnet implementation code.

Nothing here should be imported by external consumers once Moltnet becomes its own repository.

## Subpackages

- `app/`: process wiring and lifecycle
- `bridge/`: runtime bridge lifecycle and adapters
- `auth/`: auth and trust-boundary policy
- `events/`: event dispatch and subscriptions
- `rooms/`: room, thread, and DM coordination
- `store/`: persistence interfaces and backends
- `transport/`: HTTP, SSE, and later WebSocket adapters

## Rules

- Keep boundaries crisp between transport, domain logic, and persistence.
- Avoid circular package relationships.
- Put shared types in `pkg/protocol` only if they are truly public wire concepts.
