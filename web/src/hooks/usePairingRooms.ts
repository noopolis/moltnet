import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function usePairingRooms(pairingId: string | null) {
  return useQuery({
    queryKey: ["pairing", pairingId, "rooms"],
    queryFn: () => api.pairingRooms(pairingId!),
    enabled: !!pairingId,
  });
}
