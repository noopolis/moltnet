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

## Causal Contract File Inventory

- `causal.go` — native causal envelope types, stable public compatibility,
  and validation used by live Moltnet emitters.
- `causal_test.go` — native envelope compatibility and validation tests.
- `causal_canonical.go` — public canonical JSON entry points plus strict
  normalization and deterministic serialization.
- `causal_canonical_parser.go` — strict canonical JSON parser state and token
  parsing, including duplicate-key and Unicode checks.
- `causal_canonical_test.go` — canonical JSON unit tests for hostile native
  values and serialization behavior.
- `causal_contract.go` — event, stream-final, and digest-domain types and
  record-level contract validation.
- `causal_contract_fields.go` — strict record/object/field and safe-integer
  extraction helpers shared by contract validators.
- `causal_contract_test.go` — direct contract and bundle validation tests.
- `causal_bundle.go` — mixed JSONL preflight, structural tuple identity, and
  event/final association rules.
- `causal_golden_test.go` — test-only reader for Stele's frozen causal corpus
  and its canonical-byte/SHA-256 assertions.
