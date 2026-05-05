import type { Network } from "./types";

export function supportsEventStream(network: Network | null | undefined) {
  return network?.capabilities?.event_stream === "sse";
}

export function supportsDirectMessages(network: Network | null | undefined) {
  return network?.capabilities?.direct_messages === true;
}

export function supportsCursorPagination(network: Network | null | undefined) {
  return network?.capabilities?.message_pagination === "cursor";
}

export function capabilityText(value: boolean | string | null | undefined) {
  if (typeof value === "boolean") {
    return value ? "enabled" : "disabled";
  }
  const trimmed = value?.trim();
  return trimmed ? trimmed : "unsupported";
}
