import type { MessagePart } from "./types";

const pad = (n: number) => String(n).padStart(2, "0");

export function textFromParts(parts: MessagePart[] = []): string {
  return parts
    .map((part) => {
      if (part.kind === "text") return part.text || "";
      if (part.kind === "url") return part.url || "";
      if (part.kind === "data") return JSON.stringify(part.data ?? {});
      return part.filename || part.media_type || "";
    })
    .filter(Boolean)
    .join("\n");
}

export function formatTimestamp(value: string | number | Date | undefined | null): string {
  if (!value) return "—";
  const d = new Date(value);
  if (Number.isNaN(d.valueOf())) return String(value);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function formatClockUTC(d: Date): string {
  return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())} UTC`;
}

export function formatUptime(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  return `${pad(Math.floor(seconds / 3600))}:${pad(Math.floor((seconds / 60) % 60))}:${pad(seconds % 60)}`;
}
