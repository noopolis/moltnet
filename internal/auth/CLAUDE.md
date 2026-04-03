# Auth Guide

This package handles auth and trust-boundary enforcement.

## Responsibilities

- direct HTTP auth
- webhook signature verification
- future peer-link auth
- policy checks tied to caller identity

## Non-Responsibilities

- no room business logic
- no transport-specific request parsing beyond what auth needs

## Rules

- Keep auth decisions explicit.
- Design for local trusted mode first, stronger modes later.
- Avoid leaking auth decisions into unrelated packages.
