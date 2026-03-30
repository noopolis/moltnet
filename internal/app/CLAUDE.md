# App Guide

This package should wire together the Moltnet process.

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
