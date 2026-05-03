import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useLayoutEffect, useMemo, useRef } from "react";
import { useMessages } from "../../../hooks/useMessages";
import { useNetwork } from "../../../hooks/useNetwork";
import {
  isMessageTargetSelection,
  type Message,
} from "../../../lib/types";
import { useSelection } from "../../../providers";
import { Panel } from "../../Panel";
import { TimelineMessage } from "./TimelineMessage";

const SCROLL_TOP_THRESHOLD = 4;
const ESTIMATED_ROW_HEIGHT = 24;

export function Timeline() {
  const { selected } = useSelection();
  const { data: network } = useNetwork();
  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } =
    useMessages();
  const parentRef = useRef<HTMLDivElement>(null);
  const directMessagesEnabled =
    !!network && network.capabilities?.direct_messages !== false;

  const messages = useMemo<Message[]>(() => {
    if (!data) return [];
    // Pages are newest-first; flatten then reverse for chronological order.
    return data.pages.flatMap((page) => page.messages).slice().reverse();
  }, [data]);

  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ESTIMATED_ROW_HEIGHT,
    overscan: 12,
  });

  // ─── Initial scroll to bottom on selection change ───────────────────────────
  // We pin the parent's scrollTop to scrollHeight twice (RAF + RAF) so the
  // virtualizer's first measured pass is reflected before we lock in.
  const scrolledKeyRef = useRef<string | null>(null);
  useLayoutEffect(() => {
    const key = isMessageTargetSelection(selected)
      ? `${selected.kind}:${selected.id}`
      : null;
    if (!key || key === scrolledKeyRef.current) return;
    if (messages.length === 0) return;

    scrolledKeyRef.current = key;
    const node = parentRef.current;
    if (!node) return;

    const stick = () => {
      if (parentRef.current) {
        parentRef.current.scrollTop = parentRef.current.scrollHeight;
      }
    };
    stick();
    requestAnimationFrame(() => {
      stick();
      requestAnimationFrame(stick);
    });
  }, [selected, messages.length]);

  // Reset the initial-scroll guard when the user navigates away from a target.
  useEffect(() => {
    if (!isMessageTargetSelection(selected)) {
      scrolledKeyRef.current = null;
    }
  }, [selected]);

  // ─── Preserve scroll position when older pages are prepended ────────────────
  // Snapshot scrollTop + scrollHeight when fetchNextPage starts; on completion,
  // shift scrollTop down by the height that was added at the top so the user's
  // anchor message stays visually fixed.
  const wasFetchingRef = useRef(false);
  const snapshotRef = useRef<{ scrollTop: number; scrollHeight: number } | null>(
    null,
  );
  useLayoutEffect(() => {
    const node = parentRef.current;
    if (isFetchingNextPage && !wasFetchingRef.current && node) {
      snapshotRef.current = {
        scrollTop: node.scrollTop,
        scrollHeight: node.scrollHeight,
      };
    }
    if (!isFetchingNextPage && wasFetchingRef.current && node && snapshotRef.current) {
      const { scrollTop, scrollHeight } = snapshotRef.current;
      const delta = node.scrollHeight - scrollHeight;
      if (delta > 0) {
        node.scrollTop = scrollTop + delta;
      }
      snapshotRef.current = null;
    }
    wasFetchingRef.current = isFetchingNextPage;
  }, [isFetchingNextPage, messages.length]);

  // ─── Trigger fetch-older when the user scrolls near the top ─────────────────
  const startIndex = virtualizer.getVirtualItems()[0]?.index ?? 0;
  useEffect(() => {
    if (
      messages.length > 0 &&
      startIndex <= SCROLL_TOP_THRESHOLD &&
      hasNextPage &&
      !isFetchingNextPage
    ) {
      fetchNextPage();
    }
  }, [startIndex, hasNextPage, isFetchingNextPage, fetchNextPage, messages.length]);

  if (!isMessageTargetSelection(selected)) {
    const targetLabel = directMessagesEnabled ? "a room or direct channel" : "a room";
    return (
      <Panel>
        <Panel.Header>
          <Panel.Title>TIMELINE</Panel.Title>
        </Panel.Header>
        <Panel.Body>
          <p className="text-faint text-xs px-2 py-1.5">
            select {targetLabel}.
          </p>
        </Panel.Body>
      </Panel>
    );
  }

  if (selected.kind === "dm" && !directMessagesEnabled) {
    return (
      <Panel>
        <Panel.Header>
          <Panel.Title>TIMELINE</Panel.Title>
        </Panel.Header>
        <Panel.Body>
          <p className="text-faint text-xs px-2 py-1.5">
            direct channels are disabled for this network.
          </p>
        </Panel.Body>
      </Panel>
    );
  }

  const title = `${selected.kind === "room" ? "ROOM" : "DM"}: ${selected.id}`;

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>{title}</Panel.Title>
        <Panel.Count>{messages.length} messages</Panel.Count>
      </Panel.Header>
      <Panel.Body className="p-0">
        <div ref={parentRef} className="flex-1 overflow-auto">
          {messages.length === 0 ? (
            <p className="text-faint text-xs px-4 py-3">
              {isLoading ? "loading…" : "no messages yet."}
            </p>
          ) : (
            <div
              style={{
                height: `${virtualizer.getTotalSize()}px`,
                width: "100%",
                position: "relative",
              }}
            >
              {hasNextPage && isFetchingNextPage ? (
                <div className="absolute top-0 left-0 right-0 text-center text-[11px] text-faint py-2 pointer-events-none">
                  loading older…
                </div>
              ) : null}
              {virtualizer.getVirtualItems().map((virtualItem) => {
                const message = messages[virtualItem.index];
                if (!message) return null;
                return (
                  <div
                    key={virtualItem.key}
                    data-index={virtualItem.index}
                    ref={virtualizer.measureElement}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      transform: `translateY(${virtualItem.start}px)`,
                      paddingLeft: 16,
                      paddingRight: 16,
                    }}
                  >
                    <TimelineMessage message={message} />
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </Panel.Body>
    </Panel>
  );
}
