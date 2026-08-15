# Relaydeploy Guide

This package implements `moltnet relay deploy`: a minimal Cloudflare REST API
client and deploy orchestration for the relay Worker embedded from `relay/`.

## Responsibilities

- Cloudflare REST API client (token verify, account resolution, module
  worker upload with the `RelayRoom` Durable Object migration, secret
  management, workers.dev route enablement, subdomain resolution) — stdlib
  `net/http` only.
- Deploy orchestration: idempotent re-deploy semantics (keep the existing
  `RELAY_TOKEN` unless a new one is supplied), post-deploy DNS verification.
- Local relay credential storage (`.moltnet/relay.json`) so `pair invite` can
  reuse an already-deployed relay without re-flagging its URL and token.
- The bundle-freshness test guarding `relay/dist` against a stale commit
  (mirrors `internal/transport`'s web-bundle freshness test for `web/dist`).

## Non-Responsibilities

- No OAuth: `CLOUDFLARE_API_TOKEN` env is the only auth path (see PLAN.md).
- No relay protocol logic — this package only deploys the Worker, it never
  dials it. `internal/pairings/relay` is the runtime relay client.
- No CLI flag parsing or output formatting — that lives in `cmd/moltnet`.

## Rules

- stdlib only; no new Go module dependencies.
- Import `relay` (the embedded bundle) for the Worker artifact; never import
  `relay/src` or read TypeScript source.
- Keep the Cloudflare client testable via `net/http/httptest` without
  network access.
