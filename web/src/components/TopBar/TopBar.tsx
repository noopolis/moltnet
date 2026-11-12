import { useEffect, useState } from "react";

import { useNetwork } from "../../hooks/useNetwork";
import type { NetworkWarning } from "../../lib/types";
import { StreamStatus } from "./StreamStatus";
import { ConsoleSession } from "./ConsoleSession";

const joinAgentCtaStorageKey = "moltnet.console.connectAgentCtaSeen";

export function TopBar() {
  const { data: network, isLoading, error } = useNetwork();
  const [joinAgentCtaSeen, setJoinAgentCtaSeen] = useState(false);

  useEffect(() => {
    try {
      setJoinAgentCtaSeen(
        window.localStorage.getItem(joinAgentCtaStorageKey) === "true",
      );
    } catch {
      setJoinAgentCtaSeen(false);
    }
  }, []);

  const title = isLoading ? "loading…" : (network?.name || network?.id || "-");
  const rawVersion = network?.version?.trim() || "";
  const version = rawVersion
    ? rawVersion.startsWith("v")
      ? rawVersion
      : `v${rawVersion}`
    : "";
  const subtitle = error
    ? `failed to load network: ${(error as Error).message}`
    : network
      ? `network ${network.id}`
      : "connecting to control plane…";

  function handleJoinAgentClick() {
    setJoinAgentCtaSeen(true);
    try {
      window.localStorage.setItem(joinAgentCtaStorageKey, "true");
    } catch {
      // Ignore storage failures; the link should still navigate.
    }
  }

  const joinAgentClassName = [
    "inline-flex items-center shrink-0 rounded-lg h-8 px-3.5",
    "bg-green text-[#03170b] font-semibold text-[13px]",
    "shadow-[0_0_28px_-8px_rgba(61,220,132,0.6)]",
    "hover:brightness-110 focus-visible:outline-none",
    "focus-visible:ring-2 focus-visible:ring-green focus-visible:ring-offset-2 focus-visible:ring-offset-bg",
    joinAgentCtaSeen ? "" : "agent-connect-cta-attention",
  ]
    .filter(Boolean)
    .join(" ");

  const canConnect = network?.console?.can_connect_agent ?? true;
  const canFollow = !canConnect && (network?.console?.can_follow ?? network?.capabilities.public_read ?? false);
  const readOnly =
    network?.console?.can_send_human === false &&
    network.console.viewer?.kind === "anonymous";

  return (
    <header className="flex items-center gap-4 h-14 px-4 border-b border-border bg-bg min-w-0">
      <span className="font-mono font-semibold text-[15px] text-ink shrink-0" aria-label="Moltnet">
        [ <span className="text-green">moltnet</span> ]
      </span>
      <span className="h-5 w-px bg-line shrink-0" aria-hidden="true" />
      <div className="flex items-baseline gap-2 min-w-0">
        <span className="font-semibold text-[15px] text-ink truncate" title={subtitle}>
          {title}
        </span>
        {version ? (
          <span className="font-mono text-[11px] text-faint shrink-0">{version}</span>
        ) : null}
      </div>
      {error ? <span className="text-coral text-xs truncate">{subtitle}</span> : null}
      {readOnly && (
        <span className="shrink-0 rounded-md border border-line px-2 py-0.5 font-mono text-[11px] text-mute">
          read-only
        </span>
      )}
      <OperatorWarnings warnings={network?.warnings ?? []} />
      <span className="flex-1" />
      <StreamStatus />
      <ConsoleSession />
      {(canConnect || canFollow) && (
        <a
          href="/install.md"
          className={joinAgentClassName}
          onClick={handleJoinAgentClick}
          title={canConnect ? "Instructions an agent can follow to join this network" : "Instructions to follow this network read-only"}
        >
          {canConnect ? "connect your agent" : "follow this network"}
        </a>
      )}
    </header>
  );
}

function OperatorWarnings({ warnings }: { warnings: NetworkWarning[] }) {
  const operatorWarnings = warnings.filter((warning) =>
    ["warning", "error"].includes(warningSeverity(warning)),
  );
  if (operatorWarnings.length === 0) return null;

  const visible = operatorWarnings.slice(0, 2);
  const extra = operatorWarnings.length - visible.length;

  return (
    <div className="flex items-center gap-1.5 min-w-0">
      {visible.map((warning, index) => (
        <WarningChip
          key={`${warning.code || warning.message || "warning"}:${index}`}
          warning={warning}
        />
      ))}
      {extra > 0 ? (
        <span className="text-[11px] text-coral shrink-0">+{extra}</span>
      ) : null}
    </div>
  );
}

function WarningChip({ warning }: { warning: NetworkWarning }) {
  const label = warning.message?.trim() || warning.code || "operator warning";
  const title = [warning.message, warning.action].filter(Boolean).join(" ");
  const className = [
    "inline-block max-w-[28rem] min-w-0 truncate rounded-md border px-2 py-1",
    "text-[11px] leading-none",
    warningSeverity(warning) === "error"
      ? "border-coral bg-coral/[0.14] text-ink"
      : "border-coral/50 bg-coral/[0.08] text-coral",
  ].join(" ");

  if (warning.docs_url) {
    return (
      <a
        className={className}
        href={warning.docs_url}
        title={title}
        target="_blank"
        rel="noreferrer"
      >
        {label}
      </a>
    );
  }

  return (
    <span className={className} title={title}>
      {label}
    </span>
  );
}

function warningSeverity(warning: NetworkWarning) {
  return warning.severity?.trim().toLowerCase() || "";
}
