# Updater Guide

This folder contains Moltnet self-update mechanics for release tarball installs.

## Structure

- `types.go`: shared updater options, result, and install types
- `version.go`: product version normalization and comparison
- `platform.go`: OS/arch asset naming
- `release.go`: release metadata and asset fetching behind an HTTP client
- `checksum.go`: checksum manifest parsing and SHA-256 verification
- `archive.go`: safe extraction of the `moltnet` binary from release archives
- `install.go`: install detection, metadata, and binary replacement helpers
- `lock.go`: exclusive update lock acquisition and stale lock cleanup
- `update.go`: high-level update orchestration

## Rules

- Keep command parsing in `cmd/moltnet`; business logic belongs here.
- Do not mutate config files, stores, pairings, bridge runtime files, or web assets.
- Refuse mutation unless the install is known to be self-updateable.
- Keep filesystem replacement small and testable with temp install roots.
