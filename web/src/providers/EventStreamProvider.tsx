import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { MoltnetEvent } from "../lib/types";

export type EventStreamStatus = "connecting" | "open" | "error";

interface EventStreamState {
  status: EventStreamStatus;
  events: MoltnetEvent[];
}

const EventStreamContext = createContext<EventStreamState | null>(null);

const MAX_EVENTS = 1000;

export function EventStreamProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<EventStreamStatus>("connecting");
  const [events, setEvents] = useState<MoltnetEvent[]>([]);

  useEffect(() => {
    const stream = new EventSource("/v1/events/stream");

    stream.onopen = () => setStatus("open");
    stream.onerror = () => setStatus("error");

    const onMessageCreated = (raw: MessageEvent) => {
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
    };

    stream.addEventListener("message.created", onMessageCreated);

    return () => {
      stream.removeEventListener("message.created", onMessageCreated);
      stream.close();
    };
  }, [queryClient]);

  const value = useMemo(() => ({ status, events }), [status, events]);

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
