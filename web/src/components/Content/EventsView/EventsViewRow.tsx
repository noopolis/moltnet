import { formatTimestamp } from "../../../lib/format";
import type { MoltnetEvent } from "../../../lib/types";

interface EventsViewRowProps {
  event: MoltnetEvent;
}

export function EventsViewRow({ event }: EventsViewRowProps) {
  const time = formatTimestamp(event.created_at);
  const type = (event.type || "event").toUpperCase();
  const actor =
    event.agent?.agent_id ??
    event.message?.from?.id ??
    event.message?.from?.name ??
    event.pairing?.id ??
    event.room?.id ??
    event.thread?.id ??
    event.dm?.id ??
    "—";
  const target = event.agent?.target ?? event.message?.target ?? eventTarget(event);
  const channel =
    target?.kind === "dm"
      ? `dm ${target.dm_id ?? "?"}`
      : target?.kind === "room"
        ? `room ${target.room_id ?? "?"}`
        : "—";
  const wakeDetail =
    event.agent?.message_id && event.agent?.reason
      ? `${event.agent.reason} ${event.agent.message_id}`
      : event.agent?.reason;
  const detail = [
    wakeDetail,
    event.agent?.error,
    event.room?.members ? `${event.room.members.length} members` : undefined,
    event.thread?.root_message_id
      ? `root ${event.thread.root_message_id}`
      : undefined,
    event.dm?.participant_ids?.length
      ? `${event.dm.participant_ids.length} participants`
      : undefined,
    event.replay_gap?.requested_event_id
      ? `missing cursor ${event.replay_gap.requested_event_id}`
      : undefined,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="text-xs leading-relaxed py-0.5 whitespace-pre-wrap break-words">
      <span className="text-mute tabular-nums">[{time}]</span>{" "}
      <span className="text-green tracking-wider">[{type}]</span>{" "}
      <span className="text-ink font-semibold">{actor}</span>{" "}
      <span className="text-mute">→</span>{" "}
      <span className="text-sub">{channel}</span>
      {detail ? <span className="text-faint"> · {detail}</span> : null}
    </div>
  );
}

function eventTarget(event: MoltnetEvent) {
  if (event.room?.id) {
    return { kind: "room" as const, room_id: event.room.id };
  }
  if (event.thread?.room_id) {
    return { kind: "room" as const, room_id: event.thread.room_id };
  }
  if (event.dm?.id) {
    return { kind: "dm" as const, dm_id: event.dm.id };
  }
  return undefined;
}
