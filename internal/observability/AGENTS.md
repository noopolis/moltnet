# Observability Guide

This package owns shared logging, request correlation, redaction, and
metrics. It also owns the causal-event JSONL writer and the moltnet fixture
builder for the root repo's cross-repo causal conformance harness.

## Responsibilities

- structured logging helpers
- request id propagation
- safe redaction helpers for URLs and tokens
- process metrics and HTTP metrics export
- causal-event JSONL writer (`causal.go`: `CausalWriter`, seq assignment,
  `NetworkStreamID`)
- the `message.accepted` fixture builder consumed by
  `cmd/emit-causal-fixture` (`causal_fixture.go`:
  `WriteMessageAcceptedFixture`), which the root repo's
  `src/ledger/emitters.ts` invokes by path as moltnet's row in
  `src/ledger/conformance.ts`'s cross-repo conformance run

## Rules

- Keep the API small and transport-agnostic.
- Prefer stdlib facilities (`log/slog`, `context`, `net/http`) over external stacks.
- Do not let metrics or logging logic leak transport or storage policy into callers.
- Fixture-building logic (`causal_fixture.go`) must go through the real
  `CausalWriter`, never hand-roll seq assignment or envelope validation.
