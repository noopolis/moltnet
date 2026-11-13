export type WakeMode = "owner" | "mentions" | "never";
export interface Discussion {
  id: string;
  active: boolean;
  topic: string;
  participants: string[];
  max_rounds: number;
  completed_turns: number;
  expected_agent?: string;
  reason?: string;
}
export interface ConnectorStatus {
  verified: boolean;
  agent_id: string;
  configured: boolean;
  enabled: boolean;
  max_runs_per_day: number;
  attempts_today: number;
  last_status?: string;
}
export interface Automation {
  settings: { agent_id: string; mode: WakeMode }[];
  discussion?: Discussion;
  connectors: ConnectorStatus[];
}
export function automationPath(room: string) {
  return `/v1/rooms/${encodeURIComponent(room)}/automation`;
}
export async function controlRequest<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data: unknown = await response.json();
  if (!response.ok) {
    const message = data && typeof data === "object" && "error" in data ? String(data.error) : "操作失败，请重试";
    throw new Error(message);
  }
  return data as T;
}
