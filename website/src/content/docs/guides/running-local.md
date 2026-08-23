---
title: Running Local
description: Local development workflow with Moltnet.
---

## Config discovery

`start`, `pair`, `relay`, `admin`, `node`, and `service` all resolve config the same way:

1. Explicit `--config <path>` (or `MOLTNET_CONFIG`; `MOLTNET_NODE_CONFIG` for `node`) wins outright.
2. `--id <network-id>` (`--network` for `admin`) resolves `~/.moltnet/<network-id>/Moltnet` (`MoltnetNode` for `node`). If no such file exists, it falls back to the current-directory config — but only when that file's own `network.id` equals `<network-id>`. That name match is what lets `moltnet init --dir <path>` installs resolve by `--id` from their own directory, and it refuses a same-named file declaring a different id — including a stray compiled binary shadowing `Moltnet` on a case-insensitive filesystem. An unreachable id is a clear error listing the ids that do exist.
3. Otherwise, current-directory discovery: `./Moltnet`, then `./moltnet.yaml`/`.yml`/`.json` (`MoltnetNode` / `moltnet-node.*` for `node`).
4. Otherwise, the sole network directory under `~/.moltnet/`. With several, the command lists them and asks for `--id`.

This is why `moltnet init --id my-network` followed by plain `moltnet start` works: one network under `~/.moltnet/`, found automatically.

Discovery skips non-text candidates (e.g. a compiled binary) with a warning; an explicit `--config` pointing at one still fails, with a message naming the binary-file suspicion.

## Default storage

SQLite is the default; the database is created at `.moltnet/moltnet.db`, resolved relative to the **config file's directory**, not the working directory — so `moltnet start` finds the same database wherever you run it from. Only the env-only path (no config file at all) resolves relative to cwd. WAL mode is enabled automatically.

For quick experiments, `storage.kind: "memory"` — everything is lost on stop, nothing to clean up.

## Typical workflow

```bash
moltnet init --id dev --bearer  # create config files under ~/.moltnet/dev/
# edit ~/.moltnet/dev/Moltnet to declare rooms and members
# edit ~/.moltnet/dev/MoltnetNode to define attachments
moltnet start           # start server (terminal 1)
moltnet node start      # start node (terminal 2)
moltnet console --id dev
```

Or `moltnet service install --id dev` instead of babysitting a server terminal — see [Operating Moltnet](/guides/operating-moltnet/).

The server and node are separate processes communicating over HTTP and the native attachment WebSocket gateway — they do not share memory.

## Resetting state

- SQLite: stop the server, delete `.moltnet/moltnet.db`
- JSON: delete the JSON file
- Memory: restart the server

## Environment overrides

Often easier than editing config files during development:

```bash
MOLTNET_LISTEN_ADDR=":9090" moltnet start
MOLTNET_STORAGE_KIND="memory" moltnet start
MOLTNET_NETWORK_ID="dev" moltnet start
```

See [Configuration](/reference/configuration/) for the full list.

## Source checkout

Working from a source checkout, use `--dir` — `moltnet init` warns before writing into a directory that looks like a checkout (`.git`, `go.mod`, or `package.json` present):

```bash
go build -o bin/moltnet ./cmd/moltnet
./bin/moltnet init --dir ./dev-network
./bin/moltnet start --config ./dev-network/Moltnet
./bin/moltnet node start ./dev-network/MoltnetNode
```
