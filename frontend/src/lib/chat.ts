import { useEffect, useState } from "react";
import { apiJSON } from "./api";

export type ChatMessage = { id: number; sender: "customer" | "staff"; text: string; contextUrl?: string; contextLabel?: string; hasImage: boolean; createdAt: string };
export type ChatThreadData = { id: number; status: "open" | "closed"; customerReadId: number; staffReadId: number; messages: ChatMessage[]; hasMore: boolean };
export type ChatContext = { url: string; label: string };
export const chatHref = (label: string, url = window.location.pathname) => `/chat?${new URLSearchParams({ topic: label.slice(0, 180), from: url.slice(0, 500) })}`;
export function useChatUnread(staff = false, enabled = true) {
  const [count, setCount] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    const refresh = () => { if (document.visibilityState === "visible") void apiJSON<{ unread: number }>(`/api/v1/${staff ? "admin/" : ""}chat/unread`).then(data => { if (active) setCount(data.unread); }).catch(() => {}); };
    refresh();
    const timer = window.setInterval(refresh, 15000);
    window.addEventListener("focus", refresh);
    window.addEventListener("ficusin-chat-read", refresh);
    return () => { active = false; window.clearInterval(timer); window.removeEventListener("focus", refresh); window.removeEventListener("ficusin-chat-read", refresh); };
  }, [staff, enabled]);
  return count;
}

