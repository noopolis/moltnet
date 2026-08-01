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

/**
 * An intentionally opaque, two-party transport relay. The only decoded fields
 * are the envelope discriminator, correlation id, and optional routing names.
 */
export class RelayRoom extends Server<RelayEnv> {
  static options = { hibernate: false };

  private readonly networks = new Map<string, string>();

  onConnect(connection: Connection, context: ConnectionContext) {
    if (!hasBearerToken(context.request, this.env.RELAY_TOKEN)) {
      connection.close(POLICY_VIOLATION, "unauthorized");
      return;
    }

    if (this.otherConnectionCount(connection) >= 2) {
      // PartyServer awaits this hook before returning the upgrade response.
      // Defer the frame until the client socket has been established.
      setTimeout(() => connection.close(ROOM_FULL, "relay room already has two peers"), 0);
    }
  }

  onMessage(connection: Connection, message: WSMessage) {
    const envelope = decodeEnvelope(message);
    if (envelope === undefined) {
      return;
    }

    if (envelope.t === "hello") {
      const network = claimedNetwork(envelope);
      if (network !== undefined) {
        this.networks.set(connection.id, network);
      }
      return;
    }

    if ((envelope.t !== "req" && envelope.t !== "res") || typeof envelope.id !== "string") {
      return;
    }

    this.peerFor(connection, routingNetwork(envelope))?.send(message);
  }

  onClose(connection: Connection) {
    this.networks.delete(connection.id);
  }

  private otherConnectionCount(connection: Connection) {
    let count = 0;
    for (const candidate of this.getConnections()) {
      if (candidate.id !== connection.id) {
        count += 1;
      }
    }
    return count;
  }

  private peerFor(connection: Connection, targetNetwork: string | undefined) {
    for (const candidate of this.getConnections()) {
      if (candidate.id === connection.id) {
        continue;
      }
      if (targetNetwork === undefined || this.networks.get(candidate.id) === targetNetwork) {
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
  try {
    const parsed: unknown = JSON.parse(message);
    return isRecord(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
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
