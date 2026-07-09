# E2E Working Guide

This folder contains opt-in end-to-end harnesses.

Rules:
- Keep e2e suites out of the default `go test ./...` path when they require live accounts, paid APIs, or external credentials.
- Prefer Docker wrappers for runtime e2e work so the host environment only supplies credentials.
- Do not copy runtime credentials into images. Mount them at run time.
- Document exact prerequisites, commands, and expected assertions beside each harness.
