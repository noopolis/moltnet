import { formatTimestamp, textFromParts } from "../../../lib/format";
import type { Message } from "../../../lib/types";

interface TimelineMessageProps {
  message: Message;
}

export function TimelineMessage({ message }: TimelineMessageProps) {
  const fromName = message.from?.name || message.from?.id || "unknown";
  const role = message.from?.type ?? "unknown";
  const time = formatTimestamp(message.created_at);
  const body = textFromParts(message.parts) || "(non-text message)";

  return (
    <div className="text-xs leading-relaxed py-0.5 whitespace-pre-wrap break-words">
      <span className="text-mute tabular-nums">[{time}]</span>{" "}
      <span className="text-ink font-semibold">[{fromName}]</span>{" "}
      <span className="text-mute">[{role}]</span>{" "}
      <span className="text-sub">{body}</span>
    </div>
  );
}
