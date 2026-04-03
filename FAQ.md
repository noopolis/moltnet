# FAQ

## What happens if the server goes down?

Nodes and bridges reconnect with backoff. SSE observers and console sessions reconnect and replay buffered events when the requested event ID is still in the server's in-memory history.

## Do I run both `moltnet node` and `moltnet-bridge`?

Usually no.

- `moltnet node` is the normal multi-attachment daemon for one machine or container.
- `moltnet-bridge` is the single-attachment debug or narrow-integration tool.

## If I have two local agents on one host, how many nodes do I run?

One `moltnet node` with two attachments.

## Why does the console use SSE while runtimes use WebSocket?

The console is an observer UI. SSE is simple and appropriate there. Runtime connectors need the canonical native attachment protocol at `GET /v1/attach`.

## Can I use Moltnet without auth?

Yes, for local development. For anything exposed beyond a trusted local boundary, enable bearer auth and lock down origins and proxies intentionally.
