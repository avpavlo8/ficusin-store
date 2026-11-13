export const PULL_REFRESH_DISTANCE = 96;
const SCROLLABLE = /^(auto|scroll|overlay)$/;
const IGNORE_TARGET =
  'input,textarea,select,[contenteditable="true"],[role="textbox"],[role="slider"],[role="listbox"],[draggable="true"],[data-pull-refresh-ignore]';

type PullEnvironment = {
  window: Window;
  document: Document;
  enabled: boolean;
  onProgress: (distance: number) => void;
  onRefresh: () => void | Promise<void>;
};

// A pull is a page gesture only when every scroller below the finger is at
// its start. Contained message lists, galleries and editing controls own theirs.
export function canPullRefreshAt(target: Element, win: Window, doc: Document): boolean {
  if (
    !target.isConnected ||
    target.closest(IGNORE_TARGET) ||
    doc.querySelector(
      '[data-refresh-blocked="true"],dialog[open],[role="dialog"][aria-modal="true"]',
    )
  )
    return false;
  const root = doc.scrollingElement || doc.documentElement;
  if (Math.max(win.scrollY || 0, root.scrollTop) > 1) return false;
  for (let node: Element | null = target; node && node !== root; node = node.parentElement) {
    const style = win.getComputedStyle(node);
    if (style.overflowY === "hidden" && (node === doc.body || node === doc.documentElement))
      return false;
    if (SCROLLABLE.test(style.overflowX) && node.scrollWidth > node.clientWidth + 1) return false;
    if (SCROLLABLE.test(style.overflowY) && node.scrollHeight > node.clientHeight + 1) {
      if (node.scrollTop > 1 || /^(contain|none)$/.test(style.overscrollBehaviorY)) return false;
    }
  }
  return true;
}

export function installPullToRefresh({
  window: win,
  document: doc,
  enabled,
  onProgress,
  onRefresh,
}: PullEnvironment) {
  if (!enabled) return () => {};
  let disposed = false;
  let refreshing = false;
  let pull: { x: number; y: number; identifier: number; target: Element; distance: number } | null =
    null;
  const reset = () => {
    pull = null;
    if (!disposed) onProgress(0);
  };
  const start = (event: TouchEvent) => {
    reset();
    if (refreshing || event.defaultPrevented || event.touches.length !== 1) return;
    const path = event.composedPath();
    const target = path.find((node) => (node as Node).nodeType === 1) as Element | undefined;
    if (!target || !canPullRefreshAt(target, win, doc)) return;
    const point = event.touches[0];
    pull = {
      x: point.clientX,
      y: point.clientY,
      identifier: point.identifier,
      target,
      distance: 0,
    };
  };
  const move = (event: TouchEvent) => {
    if (!pull) return;
    if (
      event.defaultPrevented ||
      event.touches.length !== 1 ||
      !canPullRefreshAt(pull.target, win, doc)
    ) {
      reset();
      return;
    }
    const point = event.touches[0];
    const dx = Math.abs(point.clientX - pull.x);
    const dy = point.clientY - pull.y;
    if (point.identifier !== pull.identifier || dy < -3 || (dx > 10 && dx > dy * 0.75)) {
      reset();
      return;
    }
    if (dy <= 0 || dy < dx * 1.3) return;
    // Prevent the first downward move, before iOS claims native panning. Other
    // gestures never get cancelled and retain normal scrolling/pinch behaviour.
    if (!event.cancelable) {
      reset();
      return;
    }
    event.preventDefault();
    pull.distance = Math.min(dy, 180);
    onProgress(pull.distance);
  };
  const end = (event: TouchEvent) => {
    const ready =
      pull &&
      event.touches.length === 0 &&
      !event.defaultPrevented &&
      pull.distance >= PULL_REFRESH_DISTANCE &&
      canPullRefreshAt(pull.target, win, doc);
    if (!ready || refreshing) {
      reset();
      return;
    }
    pull = null;
    refreshing = true;
    const finished = () => {
      refreshing = false;
      reset();
    };
    try {
      Promise.resolve(onRefresh()).then(finished, finished);
    } catch {
      finished();
    }
  };
  const cancel = () => reset();
  const visibility = () => {
    if (doc.visibilityState !== "visible") reset();
  };
  const previous = doc.documentElement.getAttribute("data-pull-refresh");
  doc.documentElement.setAttribute("data-pull-refresh", "true");
  doc.addEventListener("touchstart", start, { passive: true });
  doc.addEventListener("touchmove", move, { passive: false });
  doc.addEventListener("touchend", end, { passive: true });
  doc.addEventListener("touchcancel", cancel, { passive: true });
  doc.addEventListener("visibilitychange", visibility);
  return () => {
    disposed = true;
    pull = null;
    doc.removeEventListener("touchstart", start);
    doc.removeEventListener("touchmove", move);
    doc.removeEventListener("touchend", end);
    doc.removeEventListener("touchcancel", cancel);
    doc.removeEventListener("visibilitychange", visibility);
    if (previous === null) doc.documentElement.removeAttribute("data-pull-refresh");
    else doc.documentElement.setAttribute("data-pull-refresh", previous);
  };
}
