# Protocol Guide

This package defines Moltnet's public wire protocol.

## Responsibilities

- canonical envelope types
- event kinds
- message parts and artifacts
- version markers
- direct-surface and future A2A alignment helpers

## Rules

- Keep protocol types transport-neutral.
- Do not bake HTTP handler assumptions into the data model.
- Preserve multimodality from day one.
- Prefer additive evolution and explicit versioning.

## Compatibility Goal

This package is intended to remain usable by:

- the Moltnet server
- Spawnfile integration code
- future bridge processes
- external clients
