# Codex Bridge Guide

This package adapts Moltnet attachment deliveries to the local Codex CLI.

## Rules

- Keep shared queue/session-store behavior in `internal/bridge/clisession`.
- Do not publish Codex stdout to Moltnet.
- Use `runtime.workspace_path` as the CLI working directory.
- Prefer non-interactive `codex exec`/`codex exec resume` until a stable local server protocol is integrated.
