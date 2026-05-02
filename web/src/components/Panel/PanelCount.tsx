import type { ReactNode } from "react";

export function PanelCount({ children }: { children: ReactNode }) {
  return (
    <span className="bg-bg px-2 text-[11px] text-green">
      ( {children} )
    </span>
  );
}
