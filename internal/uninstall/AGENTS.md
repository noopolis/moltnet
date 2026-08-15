# Uninstall Guide

This package backs `moltnet uninstall` (PLAN.md phase 5): stopping and
removing every installed service, deleting the running binary, and warning
about other `moltnet` copies left on PATH.

## Responsibilities

- `NetworkIDs`: enumerate every network id the uninstall plan should act on
  — the union of network directories under `~/.moltnet/`
  (`internal/app.ListNetworkIDs`) and installed unit/plist files
  (`service.Manager.InstalledNetworkIDs`), since either half of that pairing
  can be missing on its own (a hand-deleted network directory with a
  dangling service, or a hand-removed service with data still on disk).
- `OtherCopies`: scan a `$PATH`-style string for other executable `moltnet`
  files that are not the current (symlink-resolved) binary. Call this
  *after* removing the binary, paired with `DanglingSymlinks` — the caller
  resolves `currentBinary` once, before removal, so passing that same
  already-resolved path in afterward tells "this one" apart from a
  genuinely different copy just as accurately; a candidate that resolves
  back to the now-deleted binary simply fails `os.Stat` and is skipped, the
  same "nothing here" outcome `DanglingSymlinks` exists to catch instead.
- `DanglingSymlinks`: scan a `$PATH`-style string for a `moltnet` symlink
  that no longer resolves. Call this *after* removing the binary: a symlink
  to the just-deleted binary fails `os.Stat` exactly like "nothing here",
  so `OtherCopies` silently drops it instead of reporting it.
- `RemoveBinary`: delete the running executable's own file. Safe while the
  process is still executing; permission-denied bubbles up through
  `errors.Is(err, fs.ErrPermission)` for the caller to print a `sudo rm`
  fallback instead of crashing.
- `InspectHomeDir` / `PurgeHomeDir`: report on, and then remove, `~/.moltnet`
  (or a `MOLTNET_HOME` override) for `--purge`. When the root is itself a
  symlink, only the link is removed — never the resolved target — and both
  functions report that distinctly via `HomeState`/`PurgeResult` so the CLI
  layer never claims data was deleted when only a link was. Callers must
  have already confirmed this with the operator before calling
  `PurgeHomeDir`.

## Non-Responsibilities

- No CLI flag parsing, prompting, or output formatting — that lives in
  `cmd/moltnet/uninstall.go`.
- No service start/stop/uninstall mechanics of its own — those are
  `service.Manager`'s; this package only enumerates which network ids to
  call them for.
- No confirmation logic. Every function here performs its stated removal
  unconditionally when called; the "are you sure" gates live in the CLI
  layer.

## Rules

- stdlib only; no new Go module dependencies.
- Keep every exported function safe to unit test against scratch temp
  dirs and a fake `service.Runner` — never a real launchctl/systemctl, and
  never `~/.moltnet` or a repo-root `Moltnet`/`MoltnetNode`.
