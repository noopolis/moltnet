import { recordLatency } from "./latency";
import type {
  Agent,
  DirectChannel,
  MessageFrom,
  MessagePage,
  MessagePart,
  MessageTarget,
  Network,
  Pairing,
  Room,
} from "./types";

interface RawMessagePage extends Omit<MessagePage, "messages"> {
  messages?: MessagePage["messages"] | null;
}

const configuredDelay = Number.parseInt(
  import.meta.env.VITE_MOLTNET_CONSOLE_API_DELAY_MS ?? "",
  10,
);
const apiDelayMs =
  Number.isFinite(configuredDelay) && configuredDelay > 0 ? configuredDelay : 0;

export interface SendMessageBody {
  target: MessageTarget;
  from: MessageFrom;
  parts: MessagePart[];
  mentions?: string[];
}

async function getJSON<T>(path: string): Promise<T> {
  await delayForConsoleTesting();
  const start = performance.now();
  const response = await fetch(path);
  recordLatency(Math.round(performance.now() - start));
  if (!response.ok) {
    throw new Error(`${path} returned ${response.status}`);
  }
  return (await response.json()) as T;
}

function delayForConsoleTesting() {
  if (apiDelayMs <= 0) return Promise.resolve();
  return new Promise((resolve) => window.setTimeout(resolve, apiDelayMs));
}

function normalizeMessagePage(page: RawMessagePage): MessagePage {
  return {
    ...page,
    messages: page.messages ?? [],
  };
}

export const api = {
  network: () => getJSON<Network>("/v1/network"),
  rooms: () =>
    getJSON<{ rooms?: Room[] }>("/v1/rooms").then((r) => r.rooms ?? []),
  dms: () =>
    getJSON<{ dms?: DirectChannel[] }>("/v1/dms").then((r) => r.dms ?? []),
  agents: () =>
    getJSON<{ agents?: Agent[] }>("/v1/agents").then((r) => r.agents ?? []),
  pairings: () =>
    getJSON<{ pairings?: Pairing[] }>("/v1/pairings").then((r) => r.pairings ?? []),
  pairingNetwork: (id: string) =>
    getJSON<Network>(`/v1/pairings/${encodeURIComponent(id)}/network`),
  pairingRooms: (id: string) =>
    getJSON<{ rooms?: Room[] }>(`/v1/pairings/${encodeURIComponent(id)}/rooms`).then(
      (r) => r.rooms ?? [],
    ),
  pairingAgents: (id: string) =>
    getJSON<{ agents?: Agent[] }>(`/v1/pairings/${encodeURIComponent(id)}/agents`).then(
      (r) => r.agents ?? [],
    ),
  roomMessages: (id: string, before?: string) =>
    getJSON<RawMessagePage>(
      `/v1/rooms/${encodeURIComponent(id)}/messages?limit=50${
        before ? `&before=${encodeURIComponent(before)}` : ""
      }`,
    ).then(normalizeMessagePage),
  dmMessages: (id: string, before?: string) =>
    getJSON<RawMessagePage>(
      `/v1/dms/${encodeURIComponent(id)}/messages?limit=50${
        before ? `&before=${encodeURIComponent(before)}` : ""
      }`,
    ).then(normalizeMessagePage),
  sendMessage: async (body: SendMessageBody) => {
    const start = performance.now();
    const response = await fetch("/v1/messages", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    recordLatency(Math.round(performance.now() - start));
    if (!response.ok) {
      throw new Error(`send failed → ${response.status}`);
    }
    return response.json();
  },
};
