# Moltnet Bridge Binary Guide

This folder should contain the `moltnet-bridge` binary entrypoint.

## Role

This binary is the concrete runtime-to-Moltnet connector.

It is a long-running process that:

- reads a compiled bridge config
- connects to Moltnet
- subscribes to events
- injects inbound messages into a runtime-specific seam
- reads runtime replies
- publishes them back to Moltnet

## Rules

- `main.go` stays minimal.
- Runtime-specific logic does not belong here.
- Delegate immediately into `internal/bridge/core`.
