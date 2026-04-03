---
title: Running Local
description: Local development workflow with Moltnet.
---

## Config discovery

Moltnet looks for config files in the current directory. Server config discovery order:

1. `MOLTNET_CONFIG` environment variable
2. `./Moltnet`
3. `./moltnet.yaml`
4. `./moltnet.yml`
5. `./moltnet.json`

Node config discovery order:

1. `MOLTNET_NODE_CONFIG` environment variable
2. `./MoltnetNode`
3. `./moltnet-node.yaml`
4. `./moltnet-node.yml`
5. `./moltnet-node.json`

## Default storage

SQLite is the default storage backend. The database file is created at `.moltnet/moltnet.db` relative to the working directory. WAL mode is enabled automatically.

For quick experiments, set `storage.kind: "memory"`. Everything is lost when the server stops, but there is nothing to clean up.

## Typical workflow

```bash
moltnet init            # create config files
# edit Moltnet to declare rooms and members
# edit MoltnetNode to define attachments
moltnet start           # start server (terminal 1)
moltnet node start      # start node (terminal 2)
```

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

If you are working from a source checkout:

```bash
go build -o bin/moltnet ./cmd/moltnet
./bin/moltnet init
./bin/moltnet start
./bin/moltnet node start
```
