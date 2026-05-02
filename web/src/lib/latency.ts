type Listener = (latencyMs: number) => void;

const listeners = new Set<Listener>();

export function recordLatency(latencyMs: number): void {
  for (const fn of listeners) fn(latencyMs);
}

export function subscribeLatency(fn: Listener): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}
