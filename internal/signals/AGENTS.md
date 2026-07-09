# Signals Guide

This package holds shared signal-context helpers for Moltnet command entrypoints.

## Responsibilities

- create `context.Context` values that cancel on `SIGINT` and `SIGTERM`
- keep signal wiring consistent across `moltnet`, `moltnet-node`, and `moltnet-bridge`

## Non-Responsibilities

- no command parsing
- no process lifecycle logic beyond signal-triggered cancellation

## Rules

- keep the API tiny and dependency-free
- return standard library contexts only
