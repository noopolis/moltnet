import { useNetwork } from "../../hooks/useNetwork";
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
    <header className="grid grid-cols-[1fr_auto] gap-6 px-5 pt-3.5 pb-3 border-b border-border bg-bg items-center">
      <div className="flex items-center gap-3">
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
        <span className="text-xs text-mute">{subtitle}</span>
      </div>
      <StreamStatus />
    </header>
  );
}
