# Moltnet Guide

This folder is the incubating home of the Moltnet service.

Treat it as a future standalone repository that temporarily lives inside the Spawnfile repo.

## Rules

- Keep Moltnet isolated from Spawnfile implementation code.
- Do not import from `src/`.
- Prefer standard library and small dependencies.
- Keep files under 400 lines.
- Keep public protocol types separate from internal service logic.
- Write code so extraction to a new repository is mostly a move, not a rewrite.

## Structure

- `cmd/`: binary entrypoints
- `internal/`: service internals
- `pkg/`: public reusable packages, especially protocol types
- `web/`: browser-facing inspector assets and later richer UI code

## Local Design Constraints

- `cmd/` stays thin.
- Business logic belongs in `internal/`.
- Stable wire types belong in `pkg/protocol`.
- Stable bridge config types belong in `pkg/bridgeconfig`.
- Rooms, threads, DMs, and event history are core concepts from day one.
- HTTP + JSON and SSE are the first transport targets.

## Extraction Constraint

If a change would make `moltnet/` harder to move into its own repository later, do not do it.
