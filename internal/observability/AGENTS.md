# Observability Guide

This package owns shared logging, request correlation, redaction, and metrics.

## Responsibilities

- structured logging helpers
- request id propagation
- safe redaction helpers for URLs and tokens
- process metrics and HTTP metrics export

## Rules

- Keep the API small and transport-agnostic.
- Prefer stdlib facilities (`log/slog`, `context`, `net/http`) over external stacks.
- Do not let metrics or logging logic leak transport or storage policy into callers.
