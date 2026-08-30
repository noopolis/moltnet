# Moltnet Guide

This folder is the incubating home of the Moltnet service.

Treat it as a future standalone repository that temporarily lives inside the Spawnfile repo.

## Rules

- Keep Moltnet isolated from Spawnfile implementation code.
- Do not import from `src/`.
- Prefer standard library and small dependencies.
- Keep files under 400 lines.
- Keep public protocol types separate from internal service logic.
- Write code so extraction to a new repository is mostly a move, not a rewrite.
- **Every `go build` must pass `-o` to `bin/` or a temp dir.** On case-insensitive filesystems the output binary `moltnet` collides with the `Moltnet` config filename and silently replaces it — and a repo-root `Moltnet` is live user state, not a build artifact. `go build ./...` and `go build ./cmd/moltnet` are **both** destructive: Go writes each main package's executable into the current directory. "Bare" is not the dangerous property; a missing `-o` is. Prefer `go test` and `go vet`, which write nothing.

## Structure

- `cmd/`: binary entrypoints
- `internal/`: service internals
- `pkg/`: public reusable packages, especially protocol types
- `web/`: browser-facing inspector assets and later richer UI code

## Local Design Constraints

- `cmd/` stays thin.
- Business logic belongs in `internal/`.
- Stable wire types belong in `pkg/protocol`.
- Stable bridge config types belong in `pkg/bridgeconfig`.
- Rooms, threads, DMs, and event history are core concepts from day one.
- HTTP + JSON and SSE are the first transport targets.

## Extraction Constraint

If a change would make `moltnet/` harder to move into its own repository later, do not do it.

## Branches and pull requests

**Never commit to `main`.** Every change lands through a pull request, without
exception — including one-line fixes, CI configuration, documentation, and
version bumps. Work on a branch, push it, open the PR, and let CI run.

Direct commits to `main` bypass the checks that catch what local runs do not.
A zero-byte receipt store, a package that ships without its native binary, and
a two-week-red pipeline all reached `main` in this ecosystem while every local
gate was green — CI found them the first time it ran over the code.

- Branch names describe the change: `feat/…`, `fix/…`, `ci/…`, `docs/…`.
- Commit messages are conventional and single-line (`feat:`, `fix:`, `docs:`,
  `ci:`, `chore:`, `refactor:`, `test:`).
- Never add co-author lines, sign-offs, or AI attributions.
- Commit as you go rather than in one batch at the end, so history shows how
  the work progressed.
- Merge with a merge commit rather than a squash when the individual commits
  carry meaning; squashing collapses that history irreversibly.
