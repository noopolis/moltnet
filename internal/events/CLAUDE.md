# Events Guide

This package owns internal event dispatch and subscription mechanics.

## Responsibilities

- publish and subscribe semantics
- cursor and replay coordination
- fan-out to rooms, threads, DMs, and live streams

## Non-Responsibilities

- no HTTP endpoint definitions
- no room membership rules

## Rules

- Preserve ordering rules explicitly.
- Keep event delivery semantics explicit:
  - broker live fan-out is best-effort
  - broker history is replayable within its bounded in-memory window
  - SSE uses that best-effort live stream plus bounded replay on reconnect
  - the attachment gateway adds explicit ACK and resume behavior on top of broker cursors
- The public wire shape belongs in `pkg/protocol`; internal dispatch details stay here.
