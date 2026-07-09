# Client Config Guide

This package defines the local agent-facing Moltnet client configuration contract.

## Rules

- Keep the schema stable and machine-oriented.
- Prefer JSON for generated local config files.
- Support graceful migration from older Spawnfile-owned Moltnet config files.
- Do not put HTTP client logic here; this package only owns config and discovery.
