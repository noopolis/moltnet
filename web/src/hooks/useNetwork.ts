import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function useNetwork() {
  return useQuery({ queryKey: ["network"], queryFn: api.network });
}
