# Service Guide

This package generates and manages the OS service definition that keeps one
Moltnet network's server running across reboots and restarts it on crash. It
backs `moltnet service install|uninstall|start|stop|status` (PLAN.md phase
4, item 2): a launchd LaunchAgent on macOS, a systemd user unit on Linux.

## Responsibilities

- Pure, golden-tested rendering of the LaunchAgent plist (`launchd.go`) and
  the systemd user unit (`systemd.go`) from a `Spec`.
- Unit/plist file path conventions: `~/Library/LaunchAgents/dev.moltnet.<id>.plist`
  on darwin, `~/.config/systemd/user/moltnet-<id>.service` on linux.
- Lifecycle operations (install/uninstall/start/stop/restart/status) behind
  an injectable `Runner`, so tests never call a real launchctl/systemctl.
- A clear `ErrUnsupportedOS` on anything other than darwin/linux.

## Non-Responsibilities

- No CLI flag parsing or output formatting — that lives in `cmd/moltnet`.
- No config resolution — callers (cmd/moltnet) resolve the network's config
  path and pass it in via `Spec`.
- No secrets: the unit/plist only reference the config file's path, never
  its contents.

## Rules

- stdlib only; no new Go module dependencies.
- Keep `Render*` functions pure (no filesystem or command access) so they
  stay golden-testable.
- Route every external command through `Runner`, never `exec.Command`
  directly, outside of `execRunner`.
