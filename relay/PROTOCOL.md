# Relay wire protocol

Moltnet relay frames are opaque transport envelopes:

```text
<JSON routing header><newline 0x0A><opaque payload>
```

The newline and payload are present only when the payload is non-empty. A
header-only frame has no trailing newline and is valid. Production traffic uses
binary WebSocket frames. Text frames are relayed for compatibility, but are not
the primary path.

## Header

The header is one JSON object. Go emits its fields in the following forms; the
relay never creates header fields and forwards admitted `req`/`res` frame bytes
verbatim.

| Key | Type | Meaning | Producer |
| --- | --- | --- | --- |
| `t` | string | Frame kind: `hello`, `req`, or `res`. | Go client |
| `id` | string | Request/response correlation id. | Go client on `req`; Go client handling a request on `res` |
| `auth` | string | Originator's opaque pairing credential. | Go client on `req` |
| `method` | string | HTTP method requested through the pairing. | Go client on `req` |
| `path` | string | HTTP path requested through the pairing. | Go client on `req` |
| `status` | number | HTTP response status. | Go client on `res` |
| `network` | string | Origin network name, used in a `hello` frame. | Go client on `hello` |

The relay reads `t` to distinguish `hello`, `req`, and `res`, and reads `id` to
require a string correlation id for relayed requests and responses. It does not
currently read `network`: once a room admits at most two peers, network-based
target selection is unnecessary. The prior `to` field was therefore removed.
With a third connection refused once two other peers are already admitted, the
only eligible destination is the other peer, whether a target network is
supplied or not. Admission is recorded in each socket's serialized attachment
so it survives Durable Object hibernation, and the capacity check counts the
admitted peers other than the connecting socket, which the runtime has already
accepted by the time admission is decided.

The relay must never interpret `auth`: it is the originator's opaque pairing
credential and is forwarded without inspection or validation. It must likewise
never parse the payload, which is opaque bytes and need not be JSON.

## Size limits

- Routing header: **8192 bytes**. The relay drops an over-limit frame before
  parsing its header.
- Payload: **1048576 bytes (1 MiB)**. After finding a valid routing header,
  the relay drops a frame whose remaining payload exceeds this limit.

The payload limit is enforced, not merely declared. It bounds relay buffering
and memory just as it bounds Go client buffering; forwarding an over-limit frame
would let an uncooperative peer make the relay do unbounded work.

## Shared conformance data

[`testdata/golden_frames.json`](testdata/golden_frames.json) is the byte-exact
source of truth for representative frames and both limits. Go and the Worker
test their real construction/relay paths against it. This is why the two
implementations must agree: an unnoticed constant or framing drift fails a
shared golden test instead of becoming protocol folklore.
