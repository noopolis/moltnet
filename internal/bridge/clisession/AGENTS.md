# CLI Session Bridge Guide

This package contains shared Moltnet bridge machinery for local CLI-backed runtimes.

## Purpose

Codex and Claude Code are not HTTP runtimes in the first Moltnet integration. The
bridge wakes them by executing their local CLI in a configured workspace and
letting the installed Moltnet skill publish explicit messages.

## Rules

- Keep runtime-specific command arguments out of this package.
- Do not auto-publish CLI stdout to Moltnet.
- Serialize deliveries per Moltnet conversation session.
- Queue matching messages that arrive while a CLI runtime command is active, then wake the runtime with one ordered follow-up batch.
- Keep session mappings durable under the runtime workspace.

Fake-runtime tests in this package MUST synchronize with the child process by an explicit handshake (named pipe or equivalent), NEVER by `sleep` or a short wall-clock deadline. The first exec of a freshly written script file is a system-serialized cost that runs to multiple seconds under parallel package load, so any fixed short deadline is a latent failure. `writeSlowPromptRuntimeScriptWithDelay` in `queue_lifecycle_test.go` and the other script-writing helpers in this package still use `sleep` and have the same latent defect.
