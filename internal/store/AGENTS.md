# Store Guide

This package owns persistence interfaces and implementations.

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

## Backends

- memory for tests and ephemeral local runs
- JSON snapshot storage as a compatibility path
- SQLite as the local default
- Postgres for shared or hosted deployments
- object storage integration for large artifacts later
