export const SCROLL_TOP_FETCH_PX = 120;
export const SCROLL_BOTTOM_STICKY_PX = 96;

export interface ScrollSnapshot {
  scrollTop: number;
  scrollHeight: number;
}

export function isNearTop(node: HTMLElement) {
  return node.scrollTop <= SCROLL_TOP_FETCH_PX;
}

export function isNearBottom(node: HTMLElement) {
  return node.scrollHeight - node.scrollTop - node.clientHeight <= SCROLL_BOTTOM_STICKY_PX;
}

export function isTimelineAtBottom(node: HTMLElement, lastMessageID: string) {
  return isNearBottom(node) || hasRenderedMessage(node, lastMessageID);
}

function hasRenderedMessage(node: HTMLElement, messageID: string) {
  if (!messageID) return false;
  return [...node.querySelectorAll<HTMLElement>("[data-message-id]")].some(
    (item) => item.dataset.messageId === messageID,
  );
}

export function captureScrollSnapshot(node: HTMLElement): ScrollSnapshot {
  return {
    scrollTop: node.scrollTop,
    scrollHeight: node.scrollHeight,
  };
}

export function restorePrependScroll(node: HTMLElement, snapshot: ScrollSnapshot) {
  const delta = node.scrollHeight - snapshot.scrollHeight;
  node.scrollTop = snapshot.scrollTop + Math.max(0, delta);
}

export function pinToBottom(node: HTMLElement) {
  node.scrollTop = node.scrollHeight;
}
