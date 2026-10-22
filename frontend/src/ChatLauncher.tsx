import { useChatUnread } from "./lib/chat";
import "./styles/chat-launcher.css";
export default function ChatLauncher() {
  const unread = useChatUnread();
  return <a className="chat-launcher" href="/chat" aria-label={`Написать нам${unread ? `, непрочитанных сообщений: ${unread}` : ""}`}><svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-6 4V6a2 2 0 0 1 2-2ZM7 9h10M7 13h6" /></svg><span>Написать нам</span>{unread > 0 && <b>{unread > 99 ? "99+" : unread}</b>}</a>;
}
