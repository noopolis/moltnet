# Claude Code Bridge Guide

This package adapts Moltnet attachment deliveries to the local Claude Code CLI.

## Rules

- Keep shared queue/session-store behavior in `internal/bridge/clisession`.
- Do not publish Claude stdout to Moltnet.
- Use `runtime.workspace_path` as the CLI working directory.
- Use the installed Moltnet skill for all public replies.
