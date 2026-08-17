# Updater Guide

This folder contains Moltnet self-update mechanics for release-tarball
installs and, since PLAN.md phase 6c, source/dev installs built with a
stamped checkout path.

## Structure

- `types.go`: shared updater options, result, and install types
- `version.go`: product version normalization and comparison
- `platform.go`: OS/arch asset naming
- `release.go`: release metadata and asset fetching behind an HTTP client
- `checksum.go`: checksum manifest parsing and SHA-256 verification
- `archive.go`: safe extraction of the `moltnet` binary from release archives
- `install.go`: install detection, metadata, and `ReplaceBinary` (release
  tarballs only)
- `binaryreplace.go`: `replaceInstalledBinary` (source rebuilds) plus the
  install-dir writability preflight/error it shares with a source update's
  earlier pull/build steps
- `lock.go`: exclusive update lock acquisition and stale lock cleanup
- `git.go`: the injectable `CommandRunner` (git/make/fetch), read-only git
  checkout inspection, and the generated-`web/dist` exclusion pathspec a
  source update needs
- `sourceupdate.go`: source-install update orchestration — fetch, refuse
  conditions, `git pull --ff-only`, `make build`, binary replacement
- `update.go`: high-level update orchestration; branches to `sourceupdate.go`
  for `InstallMethodSource`, otherwise runs the release-tarball path
  unchanged
- `report.go`: renders `Result` (both install kinds) to text, gated by a
  `--verbose` flag for a source update's per-step detail; also previews a
  real run's refusal during `--check`/`--dry-run` and names the actual
  blocker (not a generic sentence) on a real refusal

## Source installs (`moltnet update` on a dev/source build)

- A source build only attempts a rebuild-in-place when it was built with
  the Makefile's `build` target, which stamps the checkout path via ldflags
  (`-X main.sourceCheckout=$(CURDIR)`) exactly like `main.version`. An
  unstamped binary, a moved/deleted checkout, or a checkout that is not a
  git repository all degrade to the plain "install a release tarball"
  refusal `ensureMutationAllowed` has always returned.
- `--check`/`--dry-run` AND a real run both run `git fetch` first (updates
  remote-tracking refs only, never the working tree) before comparing
  local/upstream commits — comparing against a stale `@{u}` left over from
  whatever last happened to fetch was a real bug (an available update could
  stay invisible indefinitely). If the fetch itself fails, the comparison is
  reported as "as of the last successful fetch" and a real run does not
  trust a stale "nothing to pull" reading to skip `git pull --ff-only` —
  pull gets the final say instead.
- A real run refuses outright (no auto-stash, no auto-checkout) on a dirty
  working tree or a detached HEAD, naming exactly what to fix; `--check`/
  `--dry-run` preview the identical refusal instead of promising a rebuild
  a real run would then block.
- The working-tree cleanliness check excludes the tracked `web/dist`
  directory (see `generatedWebDistPathspec` in git.go): `make build`
  regenerates it via vite's `emptyOutDir`, so without the exclusion, the
  rebuild a source update runs to verify itself would dirty the very tree
  it required to be clean.
- Otherwise: preflight the install directory's writability, `git pull
  --ff-only`, `make build` (reusing the Makefile, never reimplementing the
  build), verify the freshly built binary reports a version, then replace
  the installed binary. Every failure before the final replace leaves the
  installed binary untouched — including a failure during the replace
  itself: `replaceInstalledBinary` copies (never renames away) the current
  installed binary to its `.previous` backup, so the one rename that swaps
  the new binary in is the only mutating step ever applied to the install
  path itself.
- A source update never writes `install.json` (see `writeInstallMetadata`'s
  absence in `performSourceUpdate`): that file is one global record per
  `MOLTNET_HOME`, not one per install path, and `DetectInstall` already
  recognizes a source install from `IsDevelopmentVersion(currentVersion)`
  before it ever reads it. Writing it here would overwrite a co-resident
  release install's own metadata and disable that install's self-update.
- `--version` is accepted but has no effect on a source install (a warning
  says so): a source update always rebuilds whatever the checkout's tracked
  branch currently points at.

## Rules

- Keep command parsing in `cmd/moltnet`; business logic belongs here.
- Do not mutate config files, stores, pairings, bridge runtime files, or web assets.
- Refuse mutation unless the install is known to be self-updateable.
- Keep filesystem replacement small and testable with temp install roots.
- Route every external command (`git`, `make`) through `CommandRunner`,
  never `exec.Command` directly, outside of `defaultCommandRunner` — tests
  must never invoke a real `git`/`make` against this repository's own
  checkout.
