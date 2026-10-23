import { useLayoutEffect, useRef } from "react";
import { chatModalMedia } from "./lib/chat";

export function useAccountChatViewport() {
  const view = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    const panel = view.current;
    if (!panel) return;
    const media = window.matchMedia(chatModalMedia);
    let unlock = () => {};
    const mode = () => {
      unlock();
      if (!media.matches) return;
      const body = document.body;
      const position = { x: scrollX, y: scrollY };
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
      body.classList.add("account-chat-mobile");
      const update = () => {
        const viewport = window.visualViewport;
        panel.style.setProperty("--account-chat-height", `${viewport?.height || innerHeight}px`);
        panel.style.setProperty("--account-chat-top", `${viewport?.offsetTop || 0}px`);
      };
      update();
      window.visualViewport?.addEventListener("resize", update);
      window.visualViewport?.addEventListener("scroll", update);
      window.addEventListener("resize", update);
      unlock = () => {
        window.visualViewport?.removeEventListener("resize", update);
        window.visualViewport?.removeEventListener("scroll", update);
        window.removeEventListener("resize", update);
        body.classList.remove("account-chat-mobile");
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
  return view;
}
