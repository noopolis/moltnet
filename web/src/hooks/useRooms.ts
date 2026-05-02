import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function useRooms() {
  return useQuery({ queryKey: ["rooms"], queryFn: api.rooms });
}
