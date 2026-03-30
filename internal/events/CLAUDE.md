# Events Guide

This package should own internal event dispatch and subscription mechanics.

## Responsibilities

- publish and subscribe semantics
- cursor and replay coordination
- fan-out to rooms, threads, DMs, and live streams

## Non-Responsibilities

- no HTTP endpoint definitions
- no room membership rules

## Rules

- Preserve ordering rules explicitly.
- Keep event delivery semantics clear: best-effort, durable, replayable, or acknowledged.
- The public wire shape belongs in `pkg/protocol`; internal dispatch details stay here.
