import type { ReactNode } from "react";

interface ListItemProps {
  active?: boolean;
  showMarker?: boolean;
  onClick?: () => void;
  title: ReactNode;
  trailing?: ReactNode;
}

export function ListItem({
  active = false,
  showMarker = true,
  onClick,
  title,
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
        <span className="truncate">
          {showMarker ? (
            <span
              aria-hidden="true"
              className={`mr-1 inline-block ${active ? "text-green" : "invisible"}`}
            >
              {">"}
            </span>
          ) : null}
          {title}
        </span>
        {trailing !== undefined ? (
          <span className="text-faint text-[11px] shrink-0">{trailing}</span>
        ) : null}
      </div>
    </button>
  );
}
