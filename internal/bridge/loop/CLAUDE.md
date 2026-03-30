# Bridge Loop Guide

This package holds the generic Moltnet event loop shared by runtime adapters.

## Responsibilities

- subscribe to Moltnet event streams
- filter inbound events using bridge policy
- render runtime-facing inbound messages
- post runtime replies back into Moltnet

## Non-Responsibilities

- no runtime adapter selection
- no runtime-specific request encoding beyond the generic control bridge shape

## Rules

- keep this package runtime-agnostic
- keep wire contracts explicit and small
- avoid introducing dependencies back into `core/` or runtime adapters
