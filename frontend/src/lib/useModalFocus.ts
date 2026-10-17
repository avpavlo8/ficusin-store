import { useEffect, useRef } from "react";

export function useModalFocus(onClose: () => void, enabled = true) {
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    const panel = ref.current;
    if (!panel || !enabled) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const background: Array<[HTMLElement, boolean]> = [];
    // The drawer is nested in <main>; protect siblings at every ancestor level,
    // including the footer outside main. Preserve any existing inert state.
    let branch: HTMLElement = panel;
    while (branch.parentElement && branch.parentElement !== document.body) {
      for (const sibling of branch.parentElement.children) {
        if (sibling instanceof HTMLElement && sibling !== branch && !sibling.classList.contains("overlay")) {
          background.push([sibling, sibling.inert]);
          sibling.inert = true;
        }
      }
      branch = branch.parentElement;
    }
    const controls = () => [...panel.querySelectorAll<HTMLElement>('a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex="0"]')].filter((element) => element.getClientRects().length > 0);
    controls()[0]?.focus();
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); onClose(); }
      if (event.key !== "Tab") return;
      const items = controls();
      const first = items[0];
      const last = items.at(-1);
      if (!first || !last) { event.preventDefault(); return; }
      if (event.shiftKey && (document.activeElement === first || !panel.contains(document.activeElement))) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && (document.activeElement === last || !panel.contains(document.activeElement))) { event.preventDefault(); first.focus(); }
    };
    document.addEventListener("keydown", keydown);
    return () => {
      document.removeEventListener("keydown", keydown);
      background.forEach(([element, inert]) => { element.inert = inert; });
      if (previous?.isConnected) previous.focus();
    };
  }, [onClose, enabled]);
  return ref;
}
