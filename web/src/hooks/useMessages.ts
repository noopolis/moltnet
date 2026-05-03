import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { isMessageTargetSelection } from "../lib/types";
import { useSelection } from "../providers";
import { useNetwork } from "./useNetwork";

export function useMessages() {
  const { selected } = useSelection();
  const { data: network } = useNetwork();
  const target = isMessageTargetSelection(selected) ? selected : null;
  const directMessagesEnabled =
    !!network && network.capabilities?.direct_messages !== false;
  const targetEnabled =
    !!target && (target.kind === "room" || (!!network && directMessagesEnabled));

  return useInfiniteQuery({
    queryKey: ["messages", target?.kind, target?.id],
    enabled: targetEnabled,
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
