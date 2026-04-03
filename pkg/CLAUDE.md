# Pkg Guide

This folder is for public reusable Moltnet packages.

Use it sparingly.

## Rules

- Only place code here if external consumers should import it.
- Prefer `internal/` unless the type or helper is clearly part of the public contract.
- Keep APIs stable and version-conscious.

## Expected Public Surface

- `bridgeconfig/` for compiled bridge configuration types
- `nodeconfig/` for local multi-attachment node configuration
- `protocol/` for canonical wire types and versioned envelopes
