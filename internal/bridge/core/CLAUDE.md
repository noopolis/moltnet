# Bridge Core Guide

This package implements bridge lifecycle and adapter selection.

## Responsibilities

- load validated bridge config
- choose the runtime adapter
- own lifecycle and shutdown

## Non-Responsibilities

- no runtime-specific transport details
- no shared Moltnet stream logic; that lives in `../loop`
- no public protocol type ownership

## Rules

- keep the adapter interface small
- keep retry and backoff policy explicit
- design for one bridge process per compiled agent instance first
