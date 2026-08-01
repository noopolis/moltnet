import { routePartykitRequest, Server } from "partyserver";
import type { Connection, ConnectionContext, WSMessage } from "partyserver";

interface RelayEnv {
  RELAY_TOKEN: string;
  RelayRoom: DurableObjectNamespace;
}

type RelayEnvelope = {
  t?: unknown;
  id?: unknown;
  network?: unknown;
  to?: unknown;
};

const POLICY_VIOLATION = 1008;
const ROOM_FULL = 1013;
// Routing headers are deliberately small; this bounds newline searches on text frames.
const MAX_ROUTING_HEADER_BYTES = 8192;

/**
 * An intentionally opaque, two-party transport relay. The only decoded fields
 * are the envelope discriminator, correlation id, and optional routing names.
 */
export class RelayRoom extends Server<RelayEnv> {
  static options = { hibernate: false };

  private readonly admittedPeers = new Set<Connection>();
  private readonly networks = new Map<Connection, string>();

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

    const envelope = decodeEnvelope(message);
    if (envelope === undefined) {
      return;
    }

    if (envelope.t === "hello") {
      const network = claimedNetwork(envelope);
      if (network !== undefined) {
        this.networks.set(connection, network);
      }
      return;
    }

    if ((envelope.t !== "req" && envelope.t !== "res") || typeof envelope.id !== "string") {
      return;
    }

    const peer = this.peerFor(connection, routingNetwork(envelope));
    if (peer !== undefined && this.admittedPeers.has(peer)) {
      peer.send(message);
    }
  }

  onClose(connection: Connection) {
    this.admittedPeers.delete(connection);
    this.networks.delete(connection);
  }

  private peerFor(connection: Connection, targetNetwork: string | undefined) {
    for (const candidate of this.admittedPeers) {
      if (candidate === connection) {
        continue;
      }
      if (
        this.admittedPeers.has(candidate) &&
        (targetNetwork === undefined || this.networks.get(candidate) === targetNetwork)
      ) {
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

function decodeEnvelope(message: WSMessage): RelayEnvelope | undefined {
  if (typeof message !== "string") {
    return undefined;
  }

  const headerEnd = routingHeaderEnd(message);
  if (headerEnd === undefined) {
    return undefined;
  }

  try {
    const parsed: unknown = JSON.parse(message.slice(0, headerEnd));
    return isRecord(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
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

function claimedNetwork(envelope: RelayEnvelope) {
  return typeof envelope.network === "string" && envelope.network.length > 0
    ? envelope.network
    : undefined;
}

function routingNetwork(envelope: RelayEnvelope) {
  return typeof envelope.to === "string" && envelope.to.length > 0 ? envelope.to : undefined;
}

function isRecord(value: unknown): value is RelayEnvelope {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
