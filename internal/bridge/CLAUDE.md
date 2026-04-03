# Bridge Guide

This folder holds the Moltnet bridge implementation.

## Purpose

The bridge is the temporary compatibility layer between Moltnet and runtimes
that do not yet have native Moltnet support.

It is a real compiled process, not a skill and not an MCP server.

`moltnet-node` is the preferred multi-attachment supervisor. This package still
matters because the node is built from the same attachment runner primitives.

## Structure

- `core/`: generic bridge loop and lifecycle
- `loop/`: shared Moltnet event loop and control bridge helpers
- `tinyclaw/`: TinyClaw adapter
- `openclaw/`: OpenClaw adapter
- `picoclaw/`: PicoClaw adapter

## Rules

- Keep runtime-specific logic inside the runtime subpackages.
- Keep Moltnet transport logic in the core layer or transport packages, not mixed into adapters.
- Adapters map a stable bridge contract onto runtime-specific ingress and egress seams.
