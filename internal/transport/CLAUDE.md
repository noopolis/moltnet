# Transport Guide

This package should expose Moltnet over network transports.

## Responsibilities

- HTTP + JSON handlers
- SSE streams
- later WebSocket support
- request/response mapping to domain services

## Non-Responsibilities

- no room policy ownership
- no storage logic
- no canonical protocol type ownership

## Rules

- Keep handlers thin.
- Map transport requests into domain calls and domain events back into wire envelopes.
- Preserve a clean boundary so future transports can be added without rewriting core logic.
