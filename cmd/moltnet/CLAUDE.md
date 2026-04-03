# Moltnet CLI Guide

This folder contains the canonical `moltnet` CLI entrypoint.

## Rules

- `main.go` should stay minimal.
- No protocol definitions here.
- No storage or room logic here.
- Delegate immediately into `internal/app`, `internal/node`, or low-level attachment runners.

## Responsibilities

- parse CLI commands
- start the Moltnet server
- start the MoltnetNode daemon
- expose local client commands for agent-facing Moltnet usage
- install the canonical Moltnet skill into runtime workspaces
- expose version/help output
- handle shutdown and exit codes
