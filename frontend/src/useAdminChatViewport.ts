import { useLayoutEffect, useRef } from "react";

export function useAdminChatViewport() {
  const panel = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    const shell = panel.current?.closest<HTMLElement>(".ws-shell");
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
      const update = () => {
        shell.style.setProperty(
          "--admin-chat-height",
          `${window.visualViewport?.height || innerHeight}px`,
        );
        shell.style.setProperty("--admin-chat-top", `${window.visualViewport?.offsetTop || 0}px`);
      };
      update();
      window.visualViewport?.addEventListener("resize", update);
      window.visualViewport?.addEventListener("scroll", update);
      window.addEventListener("resize", update);
      unlock = () => {
        window.visualViewport?.removeEventListener("resize", update);
        window.visualViewport?.removeEventListener("scroll", update);
        window.removeEventListener("resize", update);
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
