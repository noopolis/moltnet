import { useEventStream } from "../../providers";

export function StreamStatus() {
  const { status, reason } = useEventStream();

  const label =
    status === "open"
      ? "live stream connected."
      : status === "unsupported"
        ? "live stream unsupported."
      : status === "error"
        ? "live stream reconnecting…"
        : "connecting to live stream…";

  const error = status === "error";
  const unsupported = status === "unsupported";
  const tone = error || unsupported ? "text-coral" : "text-green";
  const dotColor = error || unsupported ? "bg-coral" : "bg-green";
  const animate = status === "open" ? "animate-breathe" : "";

  return (
    <p
      className={`text-[11px] ${tone} inline-flex items-center gap-1.5`}
      title={reason}
    >
      <span className={`w-[7px] h-[7px] rounded-full ${dotColor} ${animate}`} />
      {label}
    </p>
  );
}
