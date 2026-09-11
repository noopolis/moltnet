# Daimon Bridge Guide

This package adapts Moltnet messages to the Daimon organization runtime's
authenticated v2 durable wake-acceptance endpoint.

## Rules

- Keep Daimon request and acceptance schemas exact and versioned.
- Resolve bearer material only at runtime and never include it in errors.
- ACK only after a matching durable acceptance and local receipt job are
  persisted. Follow cognition asynchronously, publishing terminal reply text
  with an idempotent Moltnet message id.
- A strict `noopolis.daimon.work-blocked.v1` descriptor nested in a 409
  stopped response is backpressure, not wake failure. Preserve the cursor and
  delivery identity; retry after its delay clamped to 1 second–5 minutes.
  Ordinary budget pauses still accept durable ownership through unchanged 202
  receipts. Never interpret arbitrary 409 responses as accepted or deferred.
- An explicit `moltnet send` made during a Daimon wake and the terminal receipt
  fallback share one target-scoped idempotent publication slot. The first
  durable message wins; terminal-only agents still publish through the fallback.
- Use the shared control loop for Moltnet transport.
- Do not select engines or coordinate work.
