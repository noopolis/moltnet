import type { ReactNode } from "react";

interface PanelProps {
  children: ReactNode;
  className?: string;
}

export function Panel({ children, className = "" }: PanelProps) {
  return (
    <section
      className={`relative flex flex-col min-h-0 border border-line rounded bg-bg ${className}`.trim()}
    >
      {children}
    </section>
  );
}
