import { routePartykitRequest, Server } from "partyserver";
import type { Connection, ConnectionContext, WSMessage } from "partyserver";
import { MAX_PAYLOAD_BYTES, MAX_ROUTING_HEADER_BYTES } from "./protocol.js";

interface RelayEnv {
  RELAY_TOKEN: string;
  RelayRoom: DurableObjectNamespace;
}

type RelayEnvelope = {
  t?: unknown;
  id?: unknown;
};

const POLICY_VIOLATION = 1008;
const ROOM_FULL = 1013;

/**
 * An intentionally opaque, two-party transport relay. The only decoded fields
 * are the envelope discriminator and correlation id.
 *
 * Hibernation is enabled: an idle relay room must not accrue Durable Object
 * duration charges, which are billed for the whole life of an `accept()`ed
 * socket at the 128 MiB minimum regardless of traffic. Admission state
 * therefore lives in each connection's serialized attachment, never in an
 * instance field: instance fields do not survive eviction, and a room that
 * forgets its admitted peers silently drops every frame. Protocol-level
 * ping/pong is answered by the runtime and does not wake the object.
 *
 * Invariant for future edits: no admission-relevant state in instance fields.
 */
export class RelayRoom extends Server<RelayEnv> {
  static options = { hibernate: true };

  onConnect(connection: Connection, context: ConnectionContext) {
    if (!hasBearerToken(context.request, this.env.RELAY_TOKEN)) {
      connection.close(POLICY_VIOLATION, "unauthorized");
      return;
    }

    // Under hibernation this connection is already accepted and enumerable, so
    // capacity is measured over the other admitted peers only.
    if (this.admittedPeerCount(connection) >= 2) {
      // Defer until upgrade completes; safe because refused connections are never admitted peers.
      setTimeout(() => connection.close(ROOM_FULL, "relay room already has two peers"), 0);
      return;
    }

    connection.setState({ admitted: true });
  }

  onMessage(connection: Connection, message: WSMessage) {
    if (!isAdmitted(connection)) {
      return;
    }

    const decoded = decodeEnvelope(message);
    if (decoded === undefined || decoded.payloadBytes > MAX_PAYLOAD_BYTES) {
      return;
    }
    const { envelope } = decoded;

    if (envelope.t === "hello") {
      return;
    }

    if ((envelope.t !== "req" && envelope.t !== "res") || typeof envelope.id !== "string") {
      return;
    }

    const peer = this.peerFor(connection);
    if (peer !== undefined) {
      peer.send(message);
    }
  }

  private admittedPeerCount(self: Connection) {
    let count = 0;
    for (const candidate of this.getConnections()) {
      // Object identity, never `id`: ids are client-supplied via `?_pk=` and
      // two admitted peers may collide on one.
      if (candidate !== self && isAdmitted(candidate)) {
        count += 1;
      }
    }
    return count;
  }

  private peerFor(connection: Connection) {
    for (const candidate of this.getConnections()) {
      if (candidate !== connection && isAdmitted(candidate)) {
        return candidate;
      }
    }
  }
}

function isAdmitted(connection: Connection) {
  const state: unknown = connection.state;
  return isRecord(state) && (state as { admitted?: unknown }).admitted === true;
}

export default {
  async fetch(request: Request, env: RelayEnv): Promise<Response> {
    return (
      (await routePartykitRequest(request, env, {
        onBeforeConnect(connectRequest) {
          if (hasBearerToken(connectRequest, env.RELAY_TOKEN)) {
            return;
          }
          return new Response("unauthorized", { status: 401 });
        }
      })) ?? new Response("not found", { status: 404 })
    );
  }
} satisfies ExportedHandler<RelayEnv>;

function hasBearerToken(request: Request, expectedToken: string) {
  const token = request.headers.get("authorization");
  return expectedToken.length > 0 && token === `Bearer ${expectedToken}`;
}

type DecodedEnvelope = {
  envelope: RelayEnvelope;
  payloadBytes: number;
};

function decodeEnvelope(message: WSMessage): DecodedEnvelope | undefined {
  if (typeof message === "string") {
    const headerEnd = routingHeaderEnd(message);
    if (headerEnd === undefined) return undefined;
    const envelope = parseRoutingHeader(message.slice(0, headerEnd));
    if (envelope === undefined) return undefined;
    return {
      envelope,
      payloadBytes: headerEnd === message.length ? 0 : new TextEncoder().encode(message.slice(headerEnd + 1)).byteLength
    };
  }

  const bytes = binaryMessageBytes(message);
  const headerEnd = binaryRoutingHeaderEnd(bytes);
  if (headerEnd === undefined) return undefined;
  const envelope = parseRoutingHeader(new TextDecoder().decode(bytes.subarray(0, headerEnd)));
  if (envelope === undefined) return undefined;
  return {
    envelope,
    payloadBytes: headerEnd === bytes.length ? 0 : bytes.length - headerEnd - 1
  };
}

function parseRoutingHeader(header: string): RelayEnvelope | undefined {
  try {
    const parsed: unknown = JSON.parse(header);
    return isRecord(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
}

function binaryMessageBytes(message: Exclude<WSMessage, string>) {
  return message instanceof ArrayBuffer
    ? new Uint8Array(message)
    : new Uint8Array(message.buffer, message.byteOffset, message.byteLength);
}

function binaryRoutingHeaderEnd(bytes: Uint8Array) {
  const searchEnd = Math.min(bytes.length, MAX_ROUTING_HEADER_BYTES + 1);
  for (let index = 0; index < searchEnd; index += 1) {
    if (bytes[index] === 10) {
      return index;
    }
  }
  return bytes.length <= MAX_ROUTING_HEADER_BYTES ? bytes.length : undefined;
}

function routingHeaderEnd(message: string) {
  let bytes = 0;
  for (let index = 0; index < message.length; ) {
    if (message.charCodeAt(index) === 10) {
      return index;
    }

    const codePoint = message.codePointAt(index);
    if (codePoint === undefined) {
      return undefined;
    }
    bytes += utf8ByteLength(codePoint);
    if (bytes > MAX_ROUTING_HEADER_BYTES) {
      return undefined;
    }
    index += codePoint > 0xffff ? 2 : 1;
  }
  return message.length;
}

function utf8ByteLength(codePoint: number) {
  if (codePoint <= 0x7f) return 1;
  if (codePoint <= 0x7ff) return 2;
  if (codePoint <= 0xffff) return 3;
  return 4;
}

function isRecord(value: unknown): value is RelayEnvelope {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
