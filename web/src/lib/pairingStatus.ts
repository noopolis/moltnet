import type { Pairing } from "./types";

export type PairingTone = "ok" | "pending" | "bad" | "muted";

export interface PairingDisplayStatus {
  label: string;
  tone: PairingTone;
  detail?: string;
}

const TONE_CLASS: Record<PairingTone, string> = {
  ok: "text-green",
  pending: "text-cyan",
  bad: "text-coral",
  muted: "text-faint",
};

export function pairingToneClass(tone: PairingTone): string {
  return TONE_CLASS[tone];
}

/**
 * A pairing whose peer has never answered is *pending*, not broken: the invite
 * is out but the other network hasn't joined the relay room yet. We can tell
 * the two apart because a completed handshake always records the remote's
 * version — its absence means we have never spoken to that peer.
 */
export function pairingDisplayStatus(pairing: Pairing): PairingDisplayStatus {
  const status = pairing.status?.trim() || "unknown";
  const diagnostics = pairing.diagnostics;
  const everReached = !!diagnostics?.remote_version?.trim();
  const detail =
    diagnostics?.message?.trim() ||
    diagnostics?.reason?.replaceAll("_", " ").trim() ||
    undefined;

  switch (status) {
    case "connected":
      return { label: "connected", tone: "ok", detail };
    case "pending":
      return {
        label: "pending",
        tone: "pending",
        detail: detail ?? "waiting for the peer to join",
      };
    case "degraded":
      return { label: "degraded", tone: "bad", detail };
    case "incompatible":
      return { label: "incompatible", tone: "bad", detail };
    case "error":
      return everReached
        ? { label: "unreachable", tone: "bad", detail }
        : { label: "pending", tone: "pending", detail: detail ?? "waiting for the peer to join" };
    default:
      return everReached
        ? { label: "checking", tone: "muted", detail }
        : { label: "pending", tone: "pending", detail: detail ?? "waiting for the peer to join" };
  }
}
