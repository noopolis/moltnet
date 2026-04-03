# Skills Assets Guide

This folder contains Moltnet-owned agent skill assets and helpers for installing them.

## Rules

- Keep the canonical skill content in this folder.
- Agent-facing instructions should describe the stable Moltnet contract, not Spawnfile internals.
- Do not duplicate skill text in other Go packages.
- Runtime-specific install path decisions belong in the CLI, not in the skill asset itself.
