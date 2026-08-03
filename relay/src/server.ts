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
 */
export class RelayRoom extends Server<RelayEnv> {
  static options = { hibernate: false };

  private readonly admittedPeers = new Set<Connection>();

  onConnect(connection: Connection, context: ConnectionContext) {
    if (!hasBearerToken(context.request, this.env.RELAY_TOKEN)) {
      connection.close(POLICY_VIOLATION, "unauthorized");
      return;
    }

    if (this.admittedPeers.size >= 2) {
      // Defer until upgrade completes; safe because refused connections are never admitted peers.
      setTimeout(() => connection.close(ROOM_FULL, "relay room already has two peers"), 0);
      return;
    }

    this.admittedPeers.add(connection);
  }

  onMessage(connection: Connection, message: WSMessage) {
    if (!this.admittedPeers.has(connection)) {
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
    if (peer !== undefined && this.admittedPeers.has(peer)) {
      peer.send(message);
    }
  }

  onClose(connection: Connection) {
    this.releasePeer(connection);
  }

  onError(connection: Connection, _error: unknown) {
    this.releasePeer(connection);
  }

  private releasePeer(connection: Connection) {
    this.admittedPeers.delete(connection);
  }

  private peerFor(connection: Connection) {
    for (const candidate of this.admittedPeers) {
      if (candidate === connection) {
        continue;
      }
      if (this.admittedPeers.has(candidate)) {
        return candidate;
      }
    }
  }
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
