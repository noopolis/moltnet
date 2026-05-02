import { useEventStream } from "../../providers";

export function StreamStatus() {
  const { status } = useEventStream();

  const label =
    status === "open"
      ? "live stream connected."
      : status === "error"
        ? "live stream reconnecting…"
        : "connecting to live stream…";

  const error = status === "error";
  const tone = error ? "text-coral" : "text-green";
  const dotColor = error ? "bg-coral" : "bg-green";
  const animate = status === "open" ? "animate-breathe" : "";

  return (
    <p className={`text-[11px] ${tone} inline-flex items-center gap-1.5`}>
      <span className={`w-[7px] h-[7px] rounded-full ${dotColor} ${animate}`} />
      {label}
    </p>
  );
}
