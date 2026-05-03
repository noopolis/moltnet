import { useEffect, useMemo, useState } from "react";
import { useLatency } from "../../hooks/useLatency";
import { useMessages } from "../../hooks/useMessages";
import { useNetwork } from "../../hooks/useNetwork";
import { formatClockUTC, formatUptime } from "../../lib/format";
import { useEventStream, useSelection } from "../../providers";
import { StatusItem } from "./StatusItem";

export function StatusBar() {
  const { data: network } = useNetwork();
  const latency = useLatency();
  const { data: messageData } = useMessages();
  const { events } = useEventStream();
  const { selected, select } = useSelection();
  const [bootedAt] = useState(() => Date.now());
  const [clock, setClock] = useState(() => formatClockUTC(new Date()));
  const [uptime, setUptime] = useState("00:00:00");

  useEffect(() => {
    const id = setInterval(() => {
      setClock(formatClockUTC(new Date()));
      setUptime(formatUptime(Date.now() - bootedAt));
    }, 1000);
    return () => clearInterval(id);
  }, [bootedAt]);

  const ingressOn = !!network?.capabilities?.human_ingress;
  const directMessagesEnabled =
    !!network && network.capabilities?.direct_messages !== false;
  const eventsActive = selected?.kind === "events";
  const msgs = useMemo(() => {
    if (!messageData) return 0;
    return messageData.pages.reduce((acc, page) => acc + page.messages.length, 0);
  }, [messageData]);

  const eventsButtonClass = [
    "inline-flex items-center gap-1 cursor-pointer transition-colors",
    "underline underline-offset-[3px]",
    eventsActive
      ? "text-green decoration-green"
      : "text-sub decoration-faint hover:text-green hover:decoration-green",
  ].join(" ");

  return (
    <footer className="flex justify-between gap-6 px-5 py-2 border-t border-border text-[11px] text-mute bg-bg tabular-nums flex-wrap">
      <div className="flex gap-[22px] flex-wrap items-center">
        <StatusItem label="session:" value={network?.id ?? "—"} />
        <StatusItem label="user:" value="human" />
        <StatusItem label="mode:" value="operator" />
        <StatusItem
          label="latency:"
          value={latency != null ? `${latency}ms` : "—"}
        />
      </div>
      <div className="flex gap-[22px] flex-wrap items-center">
        <StatusItem label="uptime:" value={uptime} />
        <StatusItem label="msgs:" value={String(msgs)} />
        <button
          type="button"
          className={eventsButtonClass}
          onClick={() => select({ kind: "events" })}
          aria-pressed={eventsActive}
        >
          <span>events</span>
          <span>({events.length})</span>
          <span aria-hidden="true">›</span>
        </button>
        <StatusItem
          label="stream:"
          value={network?.capabilities?.event_stream ?? "—"}
        />
        <StatusItem
          label="ingress:"
          value={ingressOn ? "enabled" : "disabled"}
          tone={ingressOn ? "good" : "default"}
        />
        <StatusItem
          label="direct:"
          value={network ? (directMessagesEnabled ? "enabled" : "disabled") : "—"}
          tone={directMessagesEnabled ? "good" : "default"}
        />
        <StatusItem value={clock} tone="ink" />
      </div>
    </footer>
  );
}
