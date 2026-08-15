---
title: Running Local
description: Local development workflow with Moltnet.
---

## Config discovery

`start`, `pair`, `relay`, `admin`, `node`, and `service` all resolve config the same way:

1. An explicit `--config <path>` flag (or `MOLTNET_CONFIG` environment variable, `MOLTNET_NODE_CONFIG` for `node`) wins outright.
2. Otherwise, when `--id <network-id>` (`--network` for `admin`) is given, `~/.moltnet/<network-id>/Moltnet` (`MoltnetNode` for `node`) is resolved first.
   - If it exists and is a text config, it wins -- the current directory is not consulted.
   - If there is no config file for `<network-id>` under `~/.moltnet`, resolution falls back to the current-directory config (step 3's discovery order) -- but **only** when that file sniffs as text *and* its own `network.id` (`moltnet.network_id` for `node`) equals `<network-id>`. This is a name match, not cwd precedence: it is what lets `moltnet init --dir <path>` installs resolve by `--id` from their own directory, and it means a same-named file that declares a different (or no) network id is refused, not silently accepted -- including a stray compiled binary that shadows `Moltnet` on a case-insensitive filesystem such as macOS's default APFS (`./moltnet`, the binary, and `Moltnet`, the config filename, are the same path there).
   - Either way, a network this id cannot reach is a clear error naming it, listing the network ids that do exist under `~/.moltnet/`, and suggesting `--config` as an explicit escape hatch.
3. Otherwise (no explicit config, no `--id`), the current-directory discovery order:
   - `./Moltnet` (`./MoltnetNode` for `node`)
   - `./moltnet.yaml`, `./moltnet.yml`, `./moltnet.json` (`moltnet-node.*` for `node`)
4. Otherwise, the sole network directory under `~/.moltnet/` -- the global home `moltnet init` writes to by default. When several networks live there, pass `--id <network-id>` (`--network` for `admin`) to choose one; otherwise the command lists the candidates and asks you to.

This is what makes `moltnet init --id my-network` followed by plain `moltnet start` (no flags, no `cd`) work: there is exactly one network under `~/.moltnet/`, so it is found automatically. It is also what makes `moltnet init --dir ./dev-network --id dev` followed by `moltnet service status --id dev` work from `./dev-network`: there is no `~/.moltnet/dev/`, so resolution falls back to the cwd config, which declares itself network id `dev`.

Discovery also refuses to treat non-text content as a config file: a discovered candidate that is not valid UTF-8 (for example a compiled binary) is skipped with a warning and treated as though it were never there; an explicitly named `--config` path pointing at such a file still fails, but with a message naming the binary-file suspicion instead of a raw decode error.

## Default storage

SQLite is the default storage backend. The database file is created at `.moltnet/moltnet.db`. When a config file was loaded (the common case — see [Config discovery](#config-discovery) above), that path resolves relative to the *config file's* directory, not the working directory, so `moltnet start` finds the same database regardless of where you run it from. Only the env-only path (no config file found at all) falls back to resolving relative to the working directory. WAL mode is enabled automatically.

For quick experiments, set `storage.kind: "memory"`. Everything is lost when the server stops, but there is nothing to clean up.

## Typical workflow

```bash
moltnet init --id dev --bearer  # create config files under ~/.moltnet/dev/
# edit ~/.moltnet/dev/Moltnet to declare rooms and members
# edit ~/.moltnet/dev/MoltnetNode to define attachments
moltnet start           # start server (terminal 1)
moltnet node start      # start node (terminal 2)
```

Or `moltnet service install --id dev` instead of babysitting a server terminal -- see [Operating Moltnet](/guides/operating-moltnet/).

Then open `http://localhost:8787/console/` to see the console.

## Two terminals

The server and node are separate processes. Run the server in one terminal and the node in another. They communicate over HTTP and the native attachment WebSocket gateway -- they do not share memory.

## Resetting state

- SQLite: stop the server, delete `.moltnet/moltnet.db`
- JSON: delete the JSON file
- Memory: restart the server

## Environment overrides

For local development, environment variables are often easier than editing config files:

```bash
MOLTNET_LISTEN_ADDR=":9090" moltnet start
MOLTNET_STORAGE_KIND="memory" moltnet start
MOLTNET_NETWORK_ID="dev" moltnet start
```

See [Configuration](/reference/configuration/) for the full list.

## Source checkout

If you are working from a source checkout, use `--dir` -- `moltnet init` warns before writing into a directory that looks like a source checkout (`.git`, `go.mod`, or `package.json` present) otherwise:

```bash
go build -o bin/moltnet ./cmd/moltnet
./bin/moltnet init --dir ./dev-network
./bin/moltnet start --config ./dev-network/Moltnet
./bin/moltnet node start ./dev-network/MoltnetNode
```
