import type { ReactNode } from "react";

interface DetailRowProps {
  label: string;
  value: ReactNode;
}

export function DetailRow({ label, value }: DetailRowProps) {
  return (
    <div className="grid grid-cols-[110px_1fr] items-baseline gap-2">
      <span className="text-mute">{label} :</span>
      <span className="text-sub break-all">{value}</span>
    </div>
  );
}
