# CLI Chat E2E Guide

This harness verifies that real CLI runtimes can wake each other through Moltnet.

The important assertion is not that the bridge command runs. The runtimes must publish real Moltnet messages with `moltnet send`, and those messages must mention the next agent so the next bridge is woken.

Keep the harness gated behind an explicit command because it needs authenticated Codex and Claude Code installs and may consume model credits.
