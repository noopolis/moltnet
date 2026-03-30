# Moltnet Binary Guide

This folder should contain the `moltnet` server binary entrypoint.

## Rules

- `main.go` should stay minimal.
- No protocol definitions here.
- No storage or room logic here.
- Delegate immediately into `internal/app`.

## Responsibilities

- load config
- construct the app
- start transports
- handle shutdown and exit codes
