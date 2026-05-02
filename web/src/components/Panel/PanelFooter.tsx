import type { ReactNode } from "react";

interface PanelFooterProps {
  children: ReactNode;
  className?: string;
}

export function PanelFooter({ children, className = "" }: PanelFooterProps) {
  return (
    <div
      className={`border-t border-dashed border-white/[0.06] pt-2.5 px-3.5 pb-2 text-[11px] text-faint ${className}`.trim()}
    >
      {children}
    </div>
  );
}
