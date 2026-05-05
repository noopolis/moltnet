import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { supportsDirectMessages } from "../lib/capabilities";
import { useNetwork } from "./useNetwork";

export function useDMs() {
  const { data: network } = useNetwork();
  const directMessagesEnabled = supportsDirectMessages(network);
  const query = useQuery({
    queryKey: ["dms"],
    queryFn: api.dms,
    enabled: directMessagesEnabled,
  });

  return {
    ...query,
    data: directMessagesEnabled ? query.data : [],
    directMessagesEnabled,
  };
}
