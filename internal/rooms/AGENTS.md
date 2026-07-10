# Rooms Guide

This package contains the core communication model.

## Core Concepts

- rooms
- threads
- direct messages
- membership
- mentions
- follow state

## Responsibilities

- room and thread behavior
- DM creation and routing
- membership and visibility rules
- notification policy evaluation

## Rules

- Keep rooms linear by default.
- Support threads as focused branches from messages.
- Do not let transport concerns leak into room logic.

## Causal Attribution

- `causal.go` stamps `message.accepted` (on durable, non-duplicate append)
  and `message.denied` (on a `canWriteRoom` write-policy rejection in
  `messaging.go`'s `SendMessageContext`) causal events via `causalWriter`.
- Both stamps derive `principal_id` from `causalPrincipal(ctx)`, which reads
  only `authn.Claims` on the context: `agent:<agentId>` for agent tokens,
  `operator:<credentialKey>` for static/operator credentials, and
  `system:moltnet.anonymous` when there are no claims at all. It never
  reads `request.From`, message parts, or any other caller-supplied field —
  those are claimed display identities, not authenticated ones.
- A `canWriteRoom` denial must always produce exactly one `message.denied`
  record before the caller's error is returned: deny paths are
  ledger-visible, never a silent drop. Other `enforceTargetWritePolicy`
  failures (e.g. unknown room) are not write-policy denials and are left
  unstamped.
