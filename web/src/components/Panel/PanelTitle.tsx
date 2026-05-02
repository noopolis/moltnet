import type { ReactNode } from "react";

export function PanelTitle({ children }: { children: ReactNode }) {
  return (
    <span className="bg-bg px-2 text-[11px] tracking-[0.12em] text-green">
      [ {children} ]
    </span>
  );
}
