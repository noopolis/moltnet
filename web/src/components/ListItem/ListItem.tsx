import type { ReactNode } from "react";

interface ListItemProps {
  active?: boolean;
  showMarker?: boolean;
  marker?: ReactNode;
  markerClassName?: string;
  onClick?: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  trailing?: ReactNode;
}

export function ListItem({
  active = false,
  showMarker = true,
  marker,
  markerClassName = "",
  onClick,
  title,
  subtitle,
  trailing,
}: ListItemProps) {
  const containerClass = [
    "block w-full text-left bg-transparent border px-2.5 py-1 cursor-pointer rounded text-xs transition-colors",
    active
      ? "bg-tint border-line-bright text-ink"
      : "border-transparent text-sub hover:bg-tint/70 hover:text-ink",
  ].join(" ");

  return (
    <button type="button" className={containerClass} onClick={onClick}>
      <div className="flex justify-between items-baseline gap-3">
        <span className="flex-1 min-w-0">
          <span className="block truncate">
            {showMarker ? (
              <span
                aria-hidden="true"
                className={`mr-1 inline-block ${markerClassName} ${active || marker !== undefined ? "" : "invisible"}`}
              >
                {active ? ">" : marker}
              </span>
            ) : null}
            {title}
          </span>
          {subtitle !== undefined ? (
            <span className="block truncate text-faint text-[11px] mt-0.5">
              {subtitle}
            </span>
          ) : null}
        </span>
        {trailing !== undefined ? (
          <span className="text-faint text-[11px] shrink-0">{trailing}</span>
        ) : null}
      </div>
    </button>
  );
}
