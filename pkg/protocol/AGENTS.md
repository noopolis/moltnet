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

## Causal Event Types

`causal.go` mirrors the cross-repo `noopolis.causal-event.v1` envelope
(canonical schema lives in root `specs/`). Moltnet emits two event types,
both stamped from `internal/rooms/causal.go`:

- `EventTypeMessageAccepted` / `MessageAcceptedPayload` — a message durably
  landed and cleared write policy (`PolicyDecisionAccepted`).
- `EventTypeMessageDenied` / `MessageDeniedPayload` — a message was
  rejected by room write policy (`PolicyDecisionDenied`); carries `target`,
  `reason`, and `content_sha256` so deny paths stay ledger-visible instead
  of a silent drop.

`principal_id` on both follows the shared grammar
(`^(agent|operator|system):.+`) and is minted only from authenticated
`authn.Claims`, never from `protocol.Actor`/`From` fields on a request.
