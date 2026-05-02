import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function usePairingNetwork(pairingId: string | null) {
  return useQuery({
    queryKey: ["pairing", pairingId, "network"],
    queryFn: () => api.pairingNetwork(pairingId!),
    enabled: !!pairingId,
  });
}
