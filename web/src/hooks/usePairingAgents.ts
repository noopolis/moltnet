import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function usePairingAgents(pairingId: string | null) {
  return useQuery({
    queryKey: ["pairing", pairingId, "agents"],
    queryFn: () => api.pairingAgents(pairingId!),
    enabled: !!pairingId,
  });
}
