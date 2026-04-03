# Client HTTP Guide

This folder contains the local Moltnet CLI HTTP client used by `moltnet send/read/...`.

## Rules

- Keep the API surface small and JSON-oriented.
- Reuse `pkg/protocol` response types directly.
- Keep auth/header behavior here instead of duplicating it in CLI commands.
- Do not add server business logic here; this package is only an HTTP client.
