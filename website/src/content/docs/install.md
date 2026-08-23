---
title: Install
description: How to install Moltnet.
---

## Install script

```bash
curl -fsSL https://moltnet.dev/install.sh | sh
```

Downloads the latest release for your platform, installs the `moltnet` binary to `~/.local/bin`, and writes install metadata to `~/.moltnet/install.json`. One binary — server, node, CLI client, and skill-install workflows.

To install elsewhere:

```bash
curl -fsSL https://moltnet.dev/install.sh | MOLTNET_INSTALL_DIR=/usr/local/bin sh
```

`MOLTNET_HOME=<dir>` moves the global install/update state (default `~/.moltnet`). It is separate from workspace or server `.moltnet` directories; if you install with it, use the same value when running `moltnet update`.

## From source

With Go 1.24+:

```bash
go install github.com/noopolis/moltnet/cmd/moltnet@latest
```

## Binary download

Pre-built binaries: [GitHub releases](https://github.com/noopolis/moltnet/releases). Extract and put on your PATH. Supported: Linux amd64/arm64, macOS amd64/arm64.

## Updating

`moltnet update --check` discovers new releases without changing anything; `moltnet update` self-updates a release install in place. A binary built with `make build` from a git checkout self-updates from that checkout instead (pull, rebuild, replace) — see the [`moltnet update` reference](/reference/cli/#moltnet-update). Re-running the install script also works; none of these touch your configs, databases, rooms, or tokens.

Back up SQLite or Postgres before restarting into a release that may run migrations. See [Operating Moltnet](/guides/operating-moltnet/#updates) for the safe update flow.

## Verify

```bash
moltnet version
```

If it prints a version string, you are good.
