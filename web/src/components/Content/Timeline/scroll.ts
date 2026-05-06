export const SCROLL_TOP_FETCH_PX = 120;
export const SCROLL_BOTTOM_STICKY_PX = 32;

export interface ScrollSnapshot {
  scrollTop: number;
  scrollHeight: number;
}

export interface ScrollAnchor {
  messageID: string;
  offsetTop: number;
}

export function isNearTop(node: HTMLElement) {
  return node.scrollTop <= SCROLL_TOP_FETCH_PX;
}

export function isNearBottom(node: HTMLElement) {
  return node.scrollHeight - node.scrollTop - node.clientHeight <= SCROLL_BOTTOM_STICKY_PX;
}

export function isTimelineAtBottom(node: HTMLElement) {
  return isNearBottom(node);
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

export function captureScrollAnchor(node: HTMLElement): ScrollAnchor | null {
  const container = node.getBoundingClientRect();
  let best: ScrollAnchor | null = null;
  let bestDistance = Number.POSITIVE_INFINITY;

  for (const item of node.querySelectorAll<HTMLElement>("[data-message-id]")) {
    const id = item.dataset.messageId;
    if (!id) continue;
    const rect = item.getBoundingClientRect();
    if (rect.bottom < container.top || rect.top > container.bottom) continue;
    const offsetTop = rect.top - container.top;
    const distance = Math.abs(offsetTop);
    if (distance < bestDistance) {
      best = { messageID: id, offsetTop };
      bestDistance = distance;
    }
  }

  return best;
}

export function restoreScrollAnchor(node: HTMLElement, anchor: ScrollAnchor) {
  for (const item of node.querySelectorAll<HTMLElement>("[data-message-id]")) {
    if (item.dataset.messageId !== anchor.messageID) continue;
    const offsetTop = item.getBoundingClientRect().top - node.getBoundingClientRect().top;
    node.scrollTop += offsetTop - anchor.offsetTop;
    return true;
  }
  return false;
}

export function pinToBottom(node: HTMLElement) {
  node.scrollTop = Math.max(0, node.scrollHeight - node.clientHeight);
}
