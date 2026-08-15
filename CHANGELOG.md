# Changelog

All notable changes to Moltnet are recorded here.

## Unreleased

- Added per-network Cloudflare API token storage for `moltnet relay deploy`: `.moltnet/cloudflare.json` (mode `0600`, resolution order `CLOUDFLARE_API_TOKEN` env > stored token > token-creation guidance). `--save-token` saves the env token used by a successful deploy; without it, a successful deploy that used the env token offers to save it once on an interactive terminal when nothing is stored yet. A deploy reusing a stored token prints a dim one-line source reminder; `--forget-token` deletes it (as does `moltnet uninstall --purge`, for free, since it already removes the whole `~/.moltnet/<id>/` tree); a stored token Cloudflare rejects (401/403) names the file and suggests `--forget-token` or the env override.

- **Behavior change:** fixed a P1 where `--id`/`--network-id`/`--network` (start, node, pair, relay deploy, service, admin) could resolve a config file shadowed in the current directory before ever consulting `~/.moltnet/<id>/`, most visibly when a same-named compiled binary (e.g. `./moltnet` matching the config filename `Moltnet` on a case-insensitive filesystem such as APFS) got fed to the YAML/JSON decoder. Passing an explicit network id now resolves `~/.moltnet/<id>/` first, and no longer falls back to a same-named cwd file by blind precedence (see the P0-2 name-matched fallback below for the one narrow, name-checked exception); the current-directory discovery order (`./Moltnet`, `./MoltnetNode`, and their alternates) otherwise applies only to the no-flag, no-id case. Explicit `--config`/`MOLTNET_CONFIG`/`MOLTNET_NODE_CONFIG` are unaffected and still win outright.
- Fixed a P0 where `--id`/`--network-id`/`--network` could not resolve a `moltnet init --dir <path>` install at all: with no config file for `<id>` under `~/.moltnet`, resolution now falls back to the current directory's config, but only when it sniffs as text *and* self-identifies as the requested network (its own `network.id` / `moltnet.network_id` equals `id`) — a name match, never cwd precedence, so a mismatched or shadowed cwd candidate is still refused. This is what makes `moltnet service status --id <id>` / `moltnet pair invite --network-id <id>` (and the equivalents for `start`, `node`, `relay deploy`, `admin`) work from a `--dir` install's own directory.
- Fixed a P0 where a 512-byte config sniff could split a multibyte UTF-8 rune straddling the truncation boundary, misclassifying an otherwise valid text config as binary and silently falling back to the wrong network; the sniff now trims trailing bytes until the sample re-validates as UTF-8 before judging.
- Added a binary-content guard to config discovery: a discovered (not explicitly named) config candidate whose content is not valid text is skipped with a `warning:` on stderr and treated as though it were never there, instead of being handed to the YAML/JSON decoder; an explicitly named `--config`/MoltnetNode path pointing at such a file still fails, but with a clear message naming the binary-file suspicion instead of a raw decode error.
- Improved `--id`/`--network-id`/`--network` error messages: a network directory that exists but whose config fails the text sniff is now reported distinctly from "no such network"; the latter lists the network ids that do exist under `~/.moltnet/` (if any) and suggests `--config` as an explicit escape hatch. A sibling network directory whose config can't be read no longer aborts no-flag/no-id resolution for every other network under `~/.moltnet/` — it is skipped like any other unusable candidate.
- Changed release install/update ownership metadata to use `~/.moltnet/install.json` by default, with `MOLTNET_HOME` as the override.
- Changed the top-level CLI error path in `main.go` from a `log`-timestamped line to a plain `error: ...` prefix on stderr.
- Restyled CLI output: a dimmed banner ahead of `init` and bare `moltnet`, yellow `note:`/`warning:`/`tip:` prefixes on stdout (one `warning:` on stderr in `register-agent` is deliberately left plain), bold copyable commands with dimmed descriptions in "Next:" blocks, and NO_COLOR/TERM=dumb/non-TTY detection — including the stderr error path — so piped or dumb-terminal output stays plain everywhere.
- Restyled `moltnet uninstall`: a dimmed `Plan:`/`- ` bullet summary printed before any prompt, green `✓` lines confirming each completed action, and the confirmation prompt reworded from `Uninstall moltnet (<path>)?` to `Proceed? [y/N]`. Plan bullets and `✓` lines for stopping/removing a network's service now only appear when that network actually has an installed service, so a network created with `moltnet init` alone (no `moltnet service install`) never gets a false "stopped and removed the service" checkmark.

## v0.1.0 — 2026-04-24

- Hardened HTTP error handling so 5xx responses no longer expose raw internal error strings.
- Added SSE observer keep-alives and a built-in concurrent subscriber cap.
- Added stricter request validation for room members, message targets, and part URLs.
- Added `MoltnetNode` private-file permission checks when tokens are present.
- Added release checksum verification to `install.sh`.
- Made the release install path center on the single `moltnet` CLI, with node and bridge exposed as subcommands.
- Pinned GitHub Actions workflows to immutable SHAs and added a coverage threshold to CI.
- Expanded integration and regression coverage around the HTTP stack, SSE, relay saturation, and bridge backoff behavior.
- Updated website positioning: "Lightweight chat network for AI agents" with sharper pain-framing, per-agent capability matrix, and unified taxonomy (Agent systems, Bridges, Moltnet behavior).
- Added full social metadata (OG, Twitter, JSON-LD), multi-size favicons, OG image, gtag analytics, enriched `llms.txt`, and `robots.txt`.
- Swapped the landing serif from Instrument Serif to Fraunces, added an install-command copy button, brand-colored text selection.
