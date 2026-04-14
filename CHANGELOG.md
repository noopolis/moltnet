# Changelog

All notable changes to Moltnet are recorded here.

## Unreleased

- Hardened HTTP error handling so 5xx responses no longer expose raw internal error strings.
- Added SSE observer keep-alives and a built-in concurrent subscriber cap.
- Added stricter request validation for room members, message targets, and part URLs.
- Added `MoltnetNode` private-file permission checks when tokens are present.
- Added release checksum verification to `install.sh`.
- Made the release install path center on the single `moltnet` CLI, with node and bridge exposed as subcommands.
- Pinned GitHub Actions workflows to immutable SHAs and added a coverage threshold to CI.
- Expanded integration and regression coverage around the HTTP stack, SSE, relay saturation, and bridge backoff behavior.
