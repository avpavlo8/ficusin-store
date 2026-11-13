import { useLayoutEffect, useRef } from "react";

export function useAdminChatViewport() {
  const panel = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    const chatPanel = panel.current;
    const shell = chatPanel?.closest<HTMLElement>(".ws-shell");
    if (!shell) return;
    const media = window.matchMedia("(max-width: 760px)");
    let unlock = () => {};
    const mode = () => {
      unlock();
      if (!media.matches) return;
      const position = { x: scrollX, y: scrollY };
      const body = document.body;
      const saved = {
        position: body.style.position,
        top: body.style.top,
        left: body.style.left,
        width: body.style.width,
      };
      Object.assign(body.style, {
        position: "fixed",
        top: `${-position.y}px`,
        left: `${-position.x}px`,
        width: "100%",
      });
      body.classList.add("admin-chat-mobile");
      let restingHeight = Math.min(window.visualViewport?.height || innerHeight, innerHeight);
      let sawKeyboard = false;
      let active = true;
      const update = () => {
        if (!active) return;
        const height = window.visualViewport?.height || innerHeight;
        const focused =
          chatPanel?.contains(document.activeElement) &&
          document.activeElement?.matches(".chat-compose textarea, .chat-reference-picker input");
        const reduced = restingHeight - height > 100;
        if (focused && reduced) sawKeyboard = true;
        const keyboard = reduced && sawKeyboard;
        if (!focused && !keyboard) restingHeight = height;
        // Hide immediately on focus; restore even when the OS dismisses its
        // keyboard without blurring the textarea (Android Back / iOS Done).
        shell.toggleAttribute("data-chat-typing", Boolean(keyboard || (focused && !sawKeyboard)));
        shell.style.setProperty(
          "--admin-chat-height",
          `${window.visualViewport?.height || innerHeight}px`,
        );
        shell.style.setProperty("--admin-chat-top", `${window.visualViewport?.offsetTop || 0}px`);
      };
      const onFocus = (event: FocusEvent) => {
        if (
          event.target instanceof Element &&
          event.target.matches(".chat-compose textarea, .chat-reference-picker input")
        ) {
          sawKeyboard = false;
        }
        update();
      };
      const onBlur = () => {
        window.requestAnimationFrame(update);
      };
      update();
      chatPanel?.addEventListener("focusin", onFocus);
      chatPanel?.addEventListener("focusout", onBlur);
      window.visualViewport?.addEventListener("resize", update);
      window.visualViewport?.addEventListener("scroll", update);
      window.addEventListener("resize", update);
      unlock = () => {
        active = false;
        window.visualViewport?.removeEventListener("resize", update);
        window.visualViewport?.removeEventListener("scroll", update);
        window.removeEventListener("resize", update);
        chatPanel?.removeEventListener("focusin", onFocus);
        chatPanel?.removeEventListener("focusout", onBlur);
        shell.removeAttribute("data-chat-typing");
        body.classList.remove("admin-chat-mobile");
        shell.style.removeProperty("--admin-chat-height");
        shell.style.removeProperty("--admin-chat-top");
        Object.assign(body.style, saved);
        window.scrollTo({ left: position.x, top: position.y, behavior: "instant" });
        unlock = () => {};
      };
    };
    mode();
    media.addEventListener("change", mode);
    return () => {
      media.removeEventListener("change", mode);
      unlock();
    };
  }, []);
  return panel;
}
