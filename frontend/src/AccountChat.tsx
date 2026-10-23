import { useEffect } from "react";
import ChatThread from "./ChatThread";
import ChatNotifications from "./ChatNotifications";
import { useAccountChatViewport } from "./useAccountChatViewport";
import type { ChatContext } from "./lib/chat";
export default function AccountChat() {
  const view = useAccountChatViewport();
  const params = new URLSearchParams(window.location.search);
  const url = params.get("from") || "";
  const context: ChatContext | undefined =
    /^\/(product|care|account\/orders)\//.test(url) && !/[\\\r\n]/.test(url)
      ? { url, label: params.get("topic") || "Вопрос по странице" }
      : undefined;
  useEffect(() => {
    document.title = "Сообщения — личный кабинет Фикусин";
  }, []);
  return (
    <section ref={view} className="account-chat-view">
      <header className="account-chat-title">
        <h1>Сообщения</h1>
        <p>Вся переписка с магазином, фотографии и ответы сотрудников.</p>
      </header>
      <div className="account-messenger telegram-chat">
        <aside className="account-chat-list" aria-label="Переписки">
          <h3>Переписки</h3>
          <a className="account-chat-contact active" href="#account-chat-conversation">
            <img className="chat-avatar" src="/assets/brand/graphite-v1/app-192-v2.png" alt="" />
            <div>
              <strong>Фикусин</strong>
              <p>Растения, уход и ваши заказы</p>
            </div>
          </a>
          <p className="account-chat-list-note">
            Сообщения из быстрого чата сохраняются здесь автоматически.
          </p>
        </aside>
        <section
          className="account-chat-conversation"
          id="account-chat-conversation"
          aria-label="Чат с магазином"
        >
          <header className="chat-panel-heading">
            <a
              className="account-chat-back"
              href="/account"
              aria-label="Вернуться в личный кабинет"
            >
              <svg
                viewBox="0 0 24 24"
                width="22"
                height="22"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.7"
                aria-hidden="true"
              >
                <path d="m14 5-7 7 7 7" />
              </svg>
            </a>
            <img className="chat-avatar" src="/assets/brand/graphite-v1/app-192-v2.png" alt="" />
            <div>
              <h2>Фикусин</h2>
              <p>Сотрудник магазина · история переписки</p>
            </div>
            <ChatNotifications />
          </header>
          <ChatThread context={context} />
        </section>
      </div>
    </section>
  );
}
