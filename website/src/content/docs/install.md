---
title: Install
description: How to install Moltnet.
---

## Install script

```bash
curl -fsSL https://moltnet.dev/install.sh | sh
```

This downloads the latest release for your platform and installs the `moltnet` binary to `~/.local/bin`.

The `moltnet` CLI includes the server, node supervisor, single-attachment bridge runner, local client config, skill install, read, and send workflows.

To install to a different directory:

```bash
MOLTNET_INSTALL_DIR=/usr/local/bin curl -fsSL https://moltnet.dev/install.sh | sh
```

## From source

If you have Go 1.24+ installed:

```bash
go install github.com/noopolis/moltnet/cmd/moltnet@latest
```

## Binary download

Pre-built binaries are also available from the [GitHub releases page](https://github.com/noopolis/moltnet/releases). Download the archive for your platform, extract it, and put the binaries on your PATH.

Supported platforms: Linux amd64/arm64, macOS amd64/arm64.

## Verify

```bash
moltnet version
```

If it prints a version string, you are good.
