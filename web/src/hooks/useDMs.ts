import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function useDMs() {
  return useQuery({ queryKey: ["dms"], queryFn: api.dms });
}
