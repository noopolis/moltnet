import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useNetwork } from "../hooks/useNetwork";
import type { MoltnetEvent } from "../lib/types";

export type EventStreamStatus = "connecting" | "open" | "error" | "unsupported";

interface EventStreamState {
  status: EventStreamStatus;
  events: MoltnetEvent[];
  reason?: string;
}

const EventStreamContext = createContext<EventStreamState | null>(null);

const MAX_EVENTS = 1000;

export function EventStreamProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const { data: network, error: networkError } = useNetwork();
  const [status, setStatus] = useState<EventStreamStatus>("connecting");
  const [reason, setReason] = useState<string | undefined>();
  const [events, setEvents] = useState<MoltnetEvent[]>([]);
  const eventStreamCapability = network?.capabilities?.event_stream;
  const hasNetwork = !!network;
  const networkErrorMessage =
    networkError instanceof Error
      ? networkError.message
      : networkError
        ? String(networkError)
        : undefined;

  useEffect(() => {
    if (networkErrorMessage) {
      setStatus("error");
      setReason(networkErrorMessage);
      return;
    }

    if (!hasNetwork) {
      setStatus("connecting");
      setReason(undefined);
      return;
    }

    if (eventStreamCapability !== "sse") {
      setStatus("unsupported");
      setReason(eventStreamUnsupportedReason(eventStreamCapability));
      return;
    }

    setStatus("connecting");
    setReason(undefined);

    const stream = new EventSource("/v1/events/stream");

    stream.onopen = () => {
      setStatus("open");
      setReason(undefined);
    };
    stream.onerror = () => {
      setStatus("error");
      setReason("event stream disconnected; browser is retrying");
    };

    const onEvent = (raw: MessageEvent) => {
      let payload: MoltnetEvent;
      try {
        payload = JSON.parse(raw.data) as MoltnetEvent;
      } catch (err) {
        console.error("invalid event payload", err);
        return;
      }

      setEvents((prev) => [payload, ...prev].slice(0, MAX_EVENTS));

      // Snapshot lists may have shifted (message_count, recent activity, etc.).
      queryClient.invalidateQueries({ queryKey: ["rooms"] });
      queryClient.invalidateQueries({ queryKey: ["dms"] });
      queryClient.invalidateQueries({ queryKey: ["agents"] });

      // Targeted message-page invalidation so the timeline refetches.
      const target = payload.message?.target;
      if (target?.kind === "room" && target.room_id) {
        queryClient.invalidateQueries({
          queryKey: ["messages", "room", target.room_id],
        });
      }
      if (target?.kind === "dm" && target.dm_id) {
        queryClient.invalidateQueries({
          queryKey: ["messages", "dm", target.dm_id],
        });
      }
      if (payload.type === "pairing.updated") {
        queryClient.invalidateQueries({ queryKey: ["pairings"] });
        queryClient.invalidateQueries({ queryKey: ["network"] });
      }
    };

    stream.addEventListener("message.created", onEvent);
    stream.addEventListener("pairing.updated", onEvent);
    stream.addEventListener("agent.connected", onEvent);
    stream.addEventListener("agent.disconnected", onEvent);
    stream.addEventListener("agent.wake.delivered", onEvent);
    stream.addEventListener("agent.wake.failed", onEvent);

    return () => {
      stream.removeEventListener("message.created", onEvent);
      stream.removeEventListener("pairing.updated", onEvent);
      stream.removeEventListener("agent.connected", onEvent);
      stream.removeEventListener("agent.disconnected", onEvent);
      stream.removeEventListener("agent.wake.delivered", onEvent);
      stream.removeEventListener("agent.wake.failed", onEvent);
      stream.close();
    };
  }, [eventStreamCapability, hasNetwork, networkErrorMessage, queryClient]);

  const value = useMemo(
    () => ({ status, events, reason }),
    [status, events, reason],
  );

  return (
    <EventStreamContext.Provider value={value}>
      {children}
    </EventStreamContext.Provider>
  );
}

export function useEventStream(): EventStreamState {
  const ctx = useContext(EventStreamContext);
  if (!ctx) {
    throw new Error("useEventStream must be used inside <EventStreamProvider>");
  }
  return ctx;
}

function eventStreamUnsupportedReason(capability: string | undefined) {
  const value = capability?.trim();
  return value
    ? `event_stream=${value} is not supported by the console`
    : "event_stream capability is not advertised";
}
