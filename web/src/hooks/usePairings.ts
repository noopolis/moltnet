import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function usePairings() {
  return useQuery({ queryKey: ["pairings"], queryFn: api.pairings });
}
