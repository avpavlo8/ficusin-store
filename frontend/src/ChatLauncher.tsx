import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { useChatUnread, type ChatContext } from "./lib/chat";
import "./styles/chat-launcher.css";
function ChatLoadError({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <>
      {open && (
        <div className="quick-chat-loading" role="alert">
          <p>Не удалось открыть чат. Обновите страницу и попробуйте снова.</p>
          <button type="button" onClick={() => window.location.reload()}>
            Обновить страницу
          </button>
          <button type="button" onClick={onClose}>
            Закрыть
          </button>
        </div>
      )}
    </>
  );
}
const QuickChat = lazy(() => import("./QuickChat").catch(() => ({ default: ChatLoadError })));

function contextOf(url: URL): ChatContext | undefined {
  const path = url.searchParams.get("from") || "";
  return /^\/(product|care|account\/orders)\//.test(path) && !/[\\\r\n]/.test(path)
    ? {
        url: path.slice(0, 500),
        label: (url.searchParams.get("topic") || "Вопрос по странице").slice(0, 180),
      }
    : undefined;
}
export default function ChatLauncher() {
  const direct = window.location.pathname === "/chat";
  const [open, setOpen] = useState(direct);
  const [started, setStarted] = useState(direct);
  const [context, setContext] = useState<ChatContext | undefined>(() =>
    direct ? contextOf(new URL(window.location.href)) : undefined,
  );
  const unread = useChatUnread();
  const trigger = useRef<HTMLElement | null>(null);
  const close = () => {
    setOpen(false);
    requestAnimationFrame(() => {
      (trigger.current?.isConnected
        ? trigger.current
        : document.querySelector<HTMLButtonElement>(".chat-launcher")
      )?.focus();
    });
  };
  useEffect(() => {
    const click = (event: MouseEvent) => {
      if (
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey ||
        event.defaultPrevented
      )
        return;
      const link =
        event.target instanceof Element ? event.target.closest<HTMLAnchorElement>("a[href]") : null;
      if (!link || link.target === "_blank") return;
      const url = new URL(link.href, window.location.href);
      if (url.origin !== window.location.origin || url.pathname !== "/chat") return;
      event.preventDefault();
      trigger.current = link;
      setContext(contextOf(url));
      setStarted(true);
      setOpen(true);
    };
    document.addEventListener("click", click);
    // Safari can pan the visual viewport independently when the keyboard opens.
    const resize = () => {
      const viewport = window.visualViewport;
      document.documentElement.style.setProperty(
        "--chat-viewport-height",
        `${viewport?.height || window.innerHeight}px`,
      );
      document.documentElement.style.setProperty(
        "--chat-viewport-top",
        `${viewport?.offsetTop || 0}px`,
      );
    };
    resize();
    window.visualViewport?.addEventListener("resize", resize);
    window.visualViewport?.addEventListener("scroll", resize);
    window.addEventListener("resize", resize);
    return () => {
      document.removeEventListener("click", click);
      window.visualViewport?.removeEventListener("resize", resize);
      window.visualViewport?.removeEventListener("scroll", resize);
      window.removeEventListener("resize", resize);
    };
  }, []);
  useEffect(() => {
    document.body.classList.toggle("quick-chat-open", open);
    return () => document.body.classList.remove("quick-chat-open");
  }, [open]);
  return (
    <>
      <button
        className="chat-launcher"
        type="button"
        aria-label={`Написать нам${unread ? `, непрочитанных сообщений: ${unread}` : ""}`}
        aria-expanded={open}
        aria-controls="quick-chat"
        onClick={(event) => {
          trigger.current = event.currentTarget;
          setStarted(true);
          setOpen((value) => !value);
        }}
      >
        <svg
          viewBox="0 0 24 24"
          width="23"
          height="23"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-6 4V6a2 2 0 0 1 2-2ZM7 9h10M7 13h6" />
        </svg>
        <span>{open ? "Свернуть чат" : "Написать нам"}</span>
        {unread > 0 && !open && <b>{unread > 99 ? "99+" : unread}</b>}
      </button>
      {started && (
        <Suspense
          fallback={
            open ? (
              <div className="quick-chat-loading" role="status">
                Открываем чат…
              </div>
            ) : null
          }
        >
          <QuickChat open={open} context={context} onClose={close} />
        </Suspense>
      )}
    </>
  );
}
