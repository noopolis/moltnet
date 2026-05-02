import { useCallback, useMemo, type ReactNode } from "react";
import { useLocation } from "wouter";
import type { Selection } from "../lib/types";

interface SelectionState {
  selected: Selection | null;
  select: (next: Selection | null) => void;
}

const ROOM_ROUTE = /^\/room\/(.+)$/;
const DM_ROUTE = /^\/dm\/(.+)$/;
const AGENT_ROUTE = /^\/agent\/(.+)$/;
const PAIRING_ROUTE = /^\/pairing\/(.+)$/;

function parseLocation(path: string): Selection | null {
  if (path === "/events") return { kind: "events" };
  const room = path.match(ROOM_ROUTE);
  if (room) return { kind: "room", id: decodeURIComponent(room[1]!) };
  const dm = path.match(DM_ROUTE);
  if (dm) return { kind: "dm", id: decodeURIComponent(dm[1]!) };
  const agent = path.match(AGENT_ROUTE);
  if (agent) return { kind: "agent", id: decodeURIComponent(agent[1]!) };
  const pairing = path.match(PAIRING_ROUTE);
  if (pairing) return { kind: "pairing", id: decodeURIComponent(pairing[1]!) };
  return null;
}

function pathFor(selection: Selection | null): string {
  if (!selection) return "/";
  if (selection.kind === "events") return "/events";
  return `/${selection.kind}/${encodeURIComponent(selection.id)}`;
}

// Keeps the SelectionProvider name + API for callers that already wrap with it,
// but the source of truth is now the URL. The component is a no-op pass-through;
// `useSelection` derives state from the router on each render.
export function SelectionProvider({ children }: { children: ReactNode }) {
  return <>{children}</>;
}

export function useSelection(): SelectionState {
  const [location, setLocation] = useLocation();

  const selected = useMemo(() => parseLocation(location), [location]);

  const select = useCallback(
    (next: Selection | null) => {
      setLocation(pathFor(next));
    },
    [setLocation],
  );

  return { selected, select };
}
