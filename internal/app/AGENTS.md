# App Guide

This package wires together the Moltnet process.

## Responsibilities

- load validated config from lower-level config objects
- construct stores, room services, and transports
- own lifecycle and shutdown ordering

## Non-Responsibilities

- no HTTP handler details
- no protocol type ownership
- no persistence-specific SQL or file layout

## Rules

- Favor dependency injection through small interfaces.
- Keep startup order explicit and testable.

## Files

- `bind_warning.go`: `IsLoopbackListenAddr` classifies a `server.listen_addr`
  value as loopback-only or not, from the bind alone — a startup-time,
  request-free heuristic, so it cannot account for a reverse proxy running
  on the same box (see its doc comment). `NonLoopbackAnonymousWriteWarning`
  builds on it: on a non-loopback bind, it warns when the effective auth
  posture leaves write/admin routes anonymously reachable — `auth.mode:
  none`, or `auth.agent_registration: open` — checked in that order so
  `mode: none` (every route anonymous) is never shadowed by the
  registration-only check. Called from `App.Run` at server start and from
  `moltnet validate` (`cmd/moltnet/validate.go`), so an operator sees the
  same warning before and after actually starting the server.
