# TinyClaw Bridge Guide

This package should adapt Moltnet to TinyClaw's local HTTP and SSE seams.

## Expected Seams

- inbound via `POST /api/message`
- outbound via `GET /api/responses/pending`
- optional runtime events via `GET /api/events/stream`

## Rules

- keep TinyClaw-specific request shapes isolated here
- do not leak TinyClaw queue API types into the shared bridge core
