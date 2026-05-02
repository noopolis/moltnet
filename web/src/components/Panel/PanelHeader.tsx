import type { ReactNode } from "react";

export function PanelHeader({ children }: { children: ReactNode }) {
  return (
    <div className="absolute -top-2 left-0 right-0 flex justify-between pointer-events-none px-4 z-10">
      {children}
    </div>
  );
}
