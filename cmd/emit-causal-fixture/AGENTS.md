# Emit Causal Fixture Binary Guide

This folder contains the `emit-causal-fixture` binary entrypoint.

## Role

`emit-causal-fixture` is moltnet's fixture emitter for the root repo's
cross-repo causal conformance harness (`src/ledger/emitters.ts`). It prints
two schema-valid `message.accepted` causal-event.v1 JSONL records to stdout,
one per line, mirroring the simfile/mneme/daimon fixture emitters.

It:

- reads `NOOPOLIS_RUN_ID` from the environment (defaults to
  `fixture-run-moltnet` when unset)
- writes exactly two JSONL lines to stdout through the real
  `internal/observability.CausalWriter`
- exits 1 with an error on stderr on any failure

## Rules

- keep `main.go` minimal
- all fixture-building logic lives in `internal/observability` (see
  `causal_fixture.go`), never here
- never write anything to stdout other than the JSONL records
