import { useEffect, useState } from "react";
import { subscribeLatency } from "../lib/latency";

export function useLatency(): number | null {
  const [latency, setLatency] = useState<number | null>(null);
  useEffect(() => subscribeLatency(setLatency), []);
  return latency;
}
