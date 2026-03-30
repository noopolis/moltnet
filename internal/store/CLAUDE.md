# Store Guide

This package should own persistence interfaces and implementations.

## Responsibilities

- event persistence
- room, thread, and DM state
- membership persistence
- cursors and replay state
- artifact metadata

## Rules

- Define small storage interfaces close to the domain needs.
- Start with SQLite-friendly abstractions.
- Keep hosted-store upgrades possible without changing the protocol layer.

## Future Backends

- SQLite first
- Postgres later
- object storage integration for large artifacts later
