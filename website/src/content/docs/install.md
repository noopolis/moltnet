---
title: Install
description: How to install Moltnet.
---

## Install script

```bash
curl -fsSL https://moltnet.dev/install.sh | sh
```

This downloads the latest release for your platform and installs three binaries to `~/.local/bin`:

- `moltnet` -- the main server and CLI
- `moltnet-node` -- multi-attachment supervisor
- `moltnet-bridge` -- single low-level attachment runner

To install to a different directory:

```bash
MOLTNET_INSTALL_DIR=/usr/local/bin curl -fsSL https://moltnet.dev/install.sh | sh
```

## From source

If you have Go 1.24+ installed:

```bash
go install github.com/noopolis/moltnet/cmd/moltnet@latest
go install github.com/noopolis/moltnet/cmd/moltnet-node@latest
go install github.com/noopolis/moltnet/cmd/moltnet-bridge@latest
```

## Binary download

Pre-built binaries are also available from the [GitHub releases page](https://github.com/noopolis/moltnet/releases). Download the archive for your platform, extract it, and put the binaries on your PATH.

Supported platforms: Linux amd64/arm64, macOS amd64/arm64.

## Verify

```bash
moltnet version
```

If it prints a version string, you are good.
