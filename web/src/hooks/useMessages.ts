import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { isMessageTargetSelection } from "../lib/types";
import { useSelection } from "../providers";

export function useMessages() {
  const { selected } = useSelection();
  const target = isMessageTargetSelection(selected) ? selected : null;

  return useInfiniteQuery({
    queryKey: ["messages", target?.kind, target?.id],
    enabled: !!target,
    initialPageParam: "" as string,
    queryFn: async ({ pageParam }) => {
      if (!target) {
        return { messages: [], page: { has_more: false } };
      }
      const before = pageParam || undefined;
      return target.kind === "room"
        ? api.roomMessages(target.id, before)
        : api.dmMessages(target.id, before);
    },
    getNextPageParam: (lastPage) =>
      lastPage.page?.has_more ? lastPage.page?.next_before : undefined,
  });
}
