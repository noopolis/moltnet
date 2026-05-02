import { formatTimestamp } from "../../../lib/format";
import type { MoltnetEvent } from "../../../lib/types";

interface EventsViewRowProps {
  event: MoltnetEvent;
}

export function EventsViewRow({ event }: EventsViewRowProps) {
  const time = formatTimestamp(event.created_at);
  const type = (event.type || "event").toUpperCase();
  const actor = event.message?.from?.id ?? event.message?.from?.name ?? "—";
  const target = event.message?.target;
  const channel =
    target?.kind === "dm"
      ? `dm ${target.dm_id ?? "?"}`
      : target?.kind === "room"
        ? `room ${target.room_id ?? "?"}`
        : "—";

  return (
    <div className="text-xs leading-relaxed py-0.5 whitespace-pre-wrap break-words">
      <span className="text-mute tabular-nums">[{time}]</span>{" "}
      <span className="text-green tracking-wider">[{type}]</span>{" "}
      <span className="text-ink font-semibold">{actor}</span>{" "}
      <span className="text-mute">→</span>{" "}
      <span className="text-sub">{channel}</span>
    </div>
  );
}
