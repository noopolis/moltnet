# Daimon Bridge Guide

This package adapts Moltnet messages to the Daimon organization runtime's
authenticated v2 durable wake-acceptance endpoint.

## Rules

- Keep Daimon request and acceptance schemas exact and versioned.
- Resolve bearer material only at runtime and never include it in errors.
- ACK only after a matching durable acceptance and local receipt job are
  persisted. Follow cognition asynchronously, publishing terminal reply text
  with an idempotent Moltnet message id.
- An explicit `moltnet send` made during a Daimon wake and the terminal receipt
  fallback share one target-scoped idempotent publication slot. The first
  durable message wins; terminal-only agents still publish through the fallback.
- Use the shared control loop for Moltnet transport.
- Do not select engines or coordinate work.
