import { useVirtualizer } from "@tanstack/react-virtual";
import { useRef } from "react";
import { useEventStream } from "../../../providers";
import { Panel } from "../../Panel";
import { EventsViewRow } from "./EventsViewRow";

export function EventsView() {
  const { events, status } = useEventStream();
  const parentRef = useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: events.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 22,
    overscan: 12,
  });

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>EVENTS</Panel.Title>
        <Panel.Count>{events.length} buffered</Panel.Count>
      </Panel.Header>
      <Panel.Body className="p-0">
        <div ref={parentRef} className="flex-1 overflow-auto">
          {events.length === 0 ? (
            <p className="text-faint text-xs px-4 py-3">
              {status === "open"
                ? "no events received yet — the stream is idle."
                : status === "error"
                  ? "stream disconnected — waiting for reconnect."
                  : "connecting to the event stream…"}
            </p>
          ) : (
            <div
              style={{
                height: `${virtualizer.getTotalSize()}px`,
                width: "100%",
                position: "relative",
              }}
            >
              {virtualizer.getVirtualItems().map((virtualItem) => {
                const event = events[virtualItem.index];
                if (!event) return null;
                return (
                  <div
                    key={virtualItem.key}
                    data-index={virtualItem.index}
                    ref={virtualizer.measureElement}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      transform: `translateY(${virtualItem.start}px)`,
                      paddingLeft: 16,
                      paddingRight: 16,
                    }}
                  >
                    <EventsViewRow event={event} />
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </Panel.Body>
    </Panel>
  );
}
