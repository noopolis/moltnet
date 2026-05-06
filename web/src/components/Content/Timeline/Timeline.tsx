import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef } from "react";
import { useMessages } from "../../../hooks/useMessages";
import { useNetwork } from "../../../hooks/useNetwork";
import { supportsDirectMessages } from "../../../lib/capabilities";
import {
  isMessageTargetSelection,
  type Message,
} from "../../../lib/types";
import { useSelection } from "../../../providers";
import { Panel } from "../../Panel";
import {
  captureScrollSnapshot,
  isNearTop,
  isTimelineAtBottom,
  pinToBottom,
  restorePrependScroll,
  type ScrollSnapshot,
} from "./scroll";
import { TimelineMessage } from "./TimelineMessage";

const ESTIMATED_ROW_HEIGHT = 24;

export function Timeline() {
  const { selected } = useSelection();
  const { data: network } = useNetwork();
  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } =
    useMessages();
  const parentRef = useRef<HTMLDivElement>(null);
  const directMessagesEnabled = supportsDirectMessages(network);
  const selectedKey = isMessageTargetSelection(selected)
    ? `${selected.kind}:${selected.id}`
    : null;

  const messages = useMemo<Message[]>(() => {
    if (!data) return [];
    // React Query stores the newest page first and appends older pages after it.
    // Each API page is already chronological, so only reverse the page groups.
    return data.pages
      .slice()
      .reverse()
      .flatMap((page) => page.messages ?? []);
  }, [data]);

  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ESTIMATED_ROW_HEIGHT,
    overscan: 12,
  });

  const firstMessageID = messages[0]?.id ?? "";
  const lastMessageID = messages[messages.length - 1]?.id ?? "";
  const nearBottomRef = useRef(true);
  const bottomPinTokenRef = useRef(0);
  const bottomPinUntilRef = useRef(0);
  const scrolledKeyRef = useRef<string | null>(null);
  const snapshotRef = useRef<ScrollSnapshot | null>(null);
  const renderedRef = useRef<{
    key: string | null;
    count: number;
    lastMessageID: string;
  }>({ key: null, count: 0, lastMessageID: "" });

  const scrollToBottom = useCallback(() => {
    const token = bottomPinTokenRef.current + 1;
    bottomPinTokenRef.current = token;
    bottomPinUntilRef.current = Date.now() + 600;
    const stick = () => {
      if (bottomPinTokenRef.current !== token) return;
      const node = parentRef.current;
      if (!node) return;
      pinToBottom(node);
      nearBottomRef.current = true;
    };

    stick();
    requestAnimationFrame(() => {
      stick();
      requestAnimationFrame(stick);
    });
    window.setTimeout(stick, 50);
    window.setTimeout(stick, 150);
    window.setTimeout(stick, 350);
  }, []);

  const maybeFetchOlder = useCallback(
    (node: HTMLDivElement | null) => {
      if (
        !node ||
        messages.length === 0 ||
        !hasNextPage ||
        isFetchingNextPage ||
        !isNearTop(node)
      ) {
        return;
      }

      snapshotRef.current = captureScrollSnapshot(node);
      void fetchNextPage();
    },
    [fetchNextPage, hasNextPage, isFetchingNextPage, messages.length],
  );

  const handleScroll = useCallback(() => {
    const node = parentRef.current;
    if (!node) return;
    const nearBottom = isTimelineAtBottom(node, lastMessageID);
    nearBottomRef.current = nearBottom;
    if (!nearBottom && Date.now() > bottomPinUntilRef.current) {
      bottomPinTokenRef.current += 1;
    }
    maybeFetchOlder(node);
  }, [lastMessageID, maybeFetchOlder]);

  // Preserve the current viewport when older pages are prepended above it.
  useLayoutEffect(() => {
    const node = parentRef.current;
    const snapshot = snapshotRef.current;
    if (isFetchingNextPage || !node || !snapshot) return;

    const restore = () => {
      const current = parentRef.current;
      if (!current) return;
      restorePrependScroll(current, snapshot);
      nearBottomRef.current = isTimelineAtBottom(current, lastMessageID);
    };

    restore();
    requestAnimationFrame(() => {
      restore();
      requestAnimationFrame(restore);
    });
    snapshotRef.current = null;
  }, [firstMessageID, isFetchingNextPage, lastMessageID, messages.length]);

  // Initial selection loads pin to the newest message. Later live appends only
  // stick when the operator was already reading at the bottom of the chat.
  useLayoutEffect(() => {
    const previous = renderedRef.current;
    const targetChanged = previous.key !== selectedKey;
    const tailChanged =
      !targetChanged &&
      lastMessageID !== "" &&
      lastMessageID !== previous.lastMessageID;
    const countGrew = !targetChanged && messages.length > previous.count;

    renderedRef.current = {
      key: selectedKey,
      count: messages.length,
      lastMessageID,
    };

    if (!selectedKey) {
      scrolledKeyRef.current = null;
      nearBottomRef.current = true;
      snapshotRef.current = null;
      return;
    }
    if (targetChanged) {
      scrolledKeyRef.current = null;
      nearBottomRef.current = true;
      snapshotRef.current = null;
    }
    if (messages.length === 0) return;
    if (scrolledKeyRef.current !== selectedKey) {
      scrolledKeyRef.current = selectedKey;
      scrollToBottom();
      return;
    }
    if (snapshotRef.current || isFetchingNextPage) return;
    const node = parentRef.current;
    const shouldStick = nearBottomRef.current || (!!node && isTimelineAtBottom(node, lastMessageID));
    if ((tailChanged || countGrew) && shouldStick) {
      scrollToBottom();
    }
  }, [isFetchingNextPage, lastMessageID, messages.length, scrollToBottom, selectedKey]);

  // ─── Trigger fetch-older when the user scrolls near the top ─────────────────
  useEffect(() => {
    maybeFetchOlder(parentRef.current);
  }, [firstMessageID, maybeFetchOlder, messages.length]);

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
        <div
          ref={parentRef}
          className="flex-1 overflow-auto"
          data-testid="timeline-scroll"
          onScroll={handleScroll}
        >
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
                    data-message-id={message.id}
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
