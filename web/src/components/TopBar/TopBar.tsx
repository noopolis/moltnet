import { useNetwork } from "../../hooks/useNetwork";
import type { NetworkWarning } from "../../lib/types";
import { StreamStatus } from "./StreamStatus";

export function TopBar() {
  const { data: network, isLoading, error } = useNetwork();

  const title = isLoading ? "loading…" : (network?.name || network?.id || "—");
  const rawVersion = network?.version?.trim() || "";
  const version = rawVersion
    ? rawVersion.startsWith("v")
      ? rawVersion
      : `v${rawVersion}`
    : "";
  const subtitle = error
    ? `failed to load network — ${(error as Error).message}`
    : network
      ? `network ${network.id}`
      : "connecting to control plane…";

  return (
    <header className="grid grid-cols-[minmax(0,1fr)_auto] gap-6 px-5 pt-3.5 pb-3 border-b border-border bg-bg items-center">
      <div className="flex items-center gap-3 min-w-0">
        <img
          src={`${import.meta.env.BASE_URL}favicon.svg`}
          alt=""
          aria-hidden="true"
          className="w-4 h-4"
        />
        <p className="text-[10px] tracking-[0.22em] text-green font-bold uppercase">
          MOLTNET
        </p>
        {version ? (
          <span className="text-[10px] tracking-[0.12em] text-mute font-bold">
            {version}
          </span>
        ) : null}
        <span className="text-[10px] tracking-[0.22em] text-ink font-bold uppercase">
          {title}
        </span>
        <span className="text-xs text-mute truncate">{subtitle}</span>
        <OperatorWarnings warnings={network?.warnings ?? []} />
      </div>
      <StreamStatus />
    </header>
  );
}

function OperatorWarnings({ warnings }: { warnings: NetworkWarning[] }) {
  const operatorWarnings = warnings.filter(
    (warning) => warning.severity === "warning" || warning.severity === "error",
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
        <span className="text-[10px] text-coral shrink-0">+{extra}</span>
      ) : null}
    </div>
  );
}

function WarningChip({ warning }: { warning: NetworkWarning }) {
  const label = warning.message?.trim() || warning.code || "operator warning";
  const title = [warning.message, warning.action].filter(Boolean).join(" ");
  const className = [
    "inline-block max-w-[28rem] min-w-0 truncate rounded border px-2 py-0.5",
    "text-[10px] leading-none",
    warning.severity === "error"
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
