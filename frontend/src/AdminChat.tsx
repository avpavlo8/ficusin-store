import { useCallback, useEffect, useRef, useState } from "react";
import ChatThread from "./ChatThread";
import ChatNotifications from "./ChatNotifications";
import { apiJSON } from "./lib/api";
import { chatRequest } from "./lib/chat-api";
import { statusLabels } from "./adminShared";

type Summary = {
  id: number;
  name: string;
  status: "open" | "closed";
  preview: string;
  unread: number;
  updatedAt: string;
  order?: { id: number; number: string; status: string };
};

type Filter = "all" | "unread" | "closed";

// Цвет аватарки постоянный для переписки: по нему глаз находит покупателя
// в списке, как в Telegram.
const avatarColors = ["#3f9b6e", "#c0743a", "#5b7fc7", "#8a6bc4", "#3f8fa6", "#c25f86", "#7b8a3a"];
function avatar(item: Pick<Summary, "id" | "name">) {
  const visitor = item.name.match(/№\s*(\d+)/);
  const letters = visitor
    ? `П${visitor[1].slice(-1)}`
    : item.name
        .split(/\s+/)
        .filter(Boolean)
        .slice(0, 2)
        .map((word) => word[0])
        .join("")
        .toUpperCase() || "?";
  return { letters, color: avatarColors[item.id % avatarColors.length] };
}

function Avatar({ item, size }: { item: Pick<Summary, "id" | "name">; size?: "small" }) {
  const { letters, color } = avatar(item);
  return (
    <span
      className="ws-chat-avatar"
      data-size={size}
      style={{ background: color }}
      aria-hidden="true"
    >
      {letters}
    </span>
  );
}

// Время в списке как в мессенджере: сегодня — часы, вчера — «вчера»,
// на этой неделе — день недели, раньше — число.
function listTime(value: string) {
  if (!value) return "";
  const date = new Date(value);
  const now = new Date();
  const days = Math.floor(
    (new Date(now.toDateString()).getTime() - new Date(date.toDateString()).getTime()) / 86400000,
  );
  if (days <= 0) return date.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
  if (days === 1) return "вчера";
  if (days < 7) return date.toLocaleDateString("ru-RU", { weekday: "short" });
  return date.toLocaleDateString("ru-RU", { day: "numeric", month: "short" });
}
export default function AdminChat() {
  const [items, setItems] = useState<Summary[]>([]);
  const [selected, setSelected] = useState<Summary | null>(() => {
    const id = Number(new URLSearchParams(location.search).get("conversation"));
    return Number.isSafeInteger(id) && id > 0
      ? { id, name: "Покупатель", status: "open", preview: "", unread: 0, updatedAt: "" }
      : null;
  });
  // Из заказа приходят с ?q=имя покупателя: сразу ищем его диалог во всех.
  const [query, setQuery] = useState(() => new URLSearchParams(location.search).get("q") || "");
  const [filter, setFilter] = useState<Filter>("all");
  const status = filter === "closed" ? "closed" : "";
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [hasMore, setHasMore] = useState(false);
  const [page, setPage] = useState(0);
  const [changing, setChanging] = useState(false);
  const requestNumber = useRef(0);
  const refresh = useCallback(async () => {
    const number = ++requestNumber.current;
    try {
      const data = await apiJSON<{ items: Summary[]; hasMore: boolean }>(
        `/api/v1/admin/chat?${new URLSearchParams({ q: query, status, offset: String(page * 50) })}`,
      );
      if (number !== requestNumber.current) return;
      setItems(data.items);
      setHasMore(data.hasMore);
      setError("");
    } catch (caught) {
      if (number === requestNumber.current) setError((caught as Error).message);
    } finally {
      if (number === requestNumber.current) setLoading(false);
    }
  }, [query, status, page]);
  useEffect(() => {
    const requests = requestNumber;
    const debounce = window.setTimeout(() => void refresh(), 250);
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") void refresh();
    }, 5000);
    window.addEventListener("focus", refresh);
    return () => {
      requests.current++;
      window.clearTimeout(debounce);
      window.clearInterval(timer);
      window.removeEventListener("focus", refresh);
    };
  }, [refresh]);
  const toggleStatus = async () => {
    if (!selected || changing) return;
    setChanging(true);
    try {
      const next =
        (items.find((item) => item.id === selected.id) || selected).status === "closed"
          ? "open"
          : "closed";
      await chatRequest(`/api/v1/admin/chat/${selected.id}`, { status: next }, "PATCH");
      setSelected({ ...selected, status: next });
      await refresh();
    } catch (caught) {
      setError((caught as Error).message);
    } finally {
      setChanging(false);
    }
  };
  const current = items.find((item) => item.id === selected?.id) || selected;
  const shown = filter === "unread" ? items.filter((item) => item.unread > 0) : items;
  const unreadChats = items.filter((item) => item.unread > 0).length;
  const filters: Array<[Filter, string]> = [
    ["all", "Все"],
    ["unread", "Непрочитанные"],
    ["closed", "Решённые"],
  ];
  return (
    <section className="admin-chat telegram-chat ws-chat">
      <h1 className="chat-sr-only">Чат с покупателями</h1>
      {error && (
        <div className="chat-error" role="alert">
          {error}
          <button onClick={() => void refresh()}>Повторить</button>
        </div>
      )}
      <div className={`admin-chat-layout ${selected ? "admin-chat-selected" : ""}`}>
        <aside className="admin-chat-inbox" aria-label="Обращения покупателей">
          <div className="ws-chat-search">
            <label>
              <span className="chat-sr-only">Найти переписку</span>
              <svg
                viewBox="0 0 24 24"
                width="16"
                height="16"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                aria-hidden="true"
              >
                <circle cx="11" cy="11" r="7" />
                <path d="m20 20-3.5-3.5" />
              </svg>
              <input
                type="search"
                value={query}
                maxLength={100}
                placeholder="Поиск"
                onChange={(event) => {
                  setQuery(event.target.value);
                  setPage(0);
                }}
              />
            </label>
            <ChatNotifications staff />
          </div>
          <nav className="ws-chat-filters" aria-label="Какие переписки показать">
            {filters.map(([value, label]) => (
              <button
                key={value}
                type="button"
                aria-pressed={filter === value}
                onClick={() => {
                  setFilter(value);
                  setPage(0);
                }}
              >
                {label}
                {value === "unread" && unreadChats > 0 && <b>{unreadChats}</b>}
              </button>
            ))}
          </nav>
          <div className="admin-chat-list">
            {loading && (
              <p className="chat-state" role="status">
                Загружаем переписки…
              </p>
            )}
            {!loading && !shown.length && (
              <p className="chat-state">
                {query
                  ? "Переписок по этому запросу нет."
                  : filter === "unread"
                    ? "Все сообщения прочитаны."
                    : "Здесь появятся сообщения покупателей."}
              </p>
            )}
            {shown.map((item) => (
              <button
                type="button"
                className={`admin-chat-item ${selected?.id === item.id ? "active" : ""}`}
                key={item.id}
                aria-pressed={selected?.id === item.id}
                onClick={() => setSelected(item)}
              >
                <Avatar item={item} />
                <span className="ws-chat-item-body">
                  <span>
                    <strong>{item.name}</strong>
                    <time dateTime={item.updatedAt}>{listTime(item.updatedAt)}</time>
                  </span>
                  <span>
                    <span className="ws-chat-preview">{item.preview}</span>
                    {item.unread > 0 ? (
                      <b aria-label={`${item.unread} новых`}>{item.unread}</b>
                    ) : item.status === "closed" ? (
                      <small>Решён</small>
                    ) : null}
                  </span>
                </span>
              </button>
            ))}
          </div>
          {(page > 0 || hasMore) && (
            <nav className="admin-chat-pages" aria-label="Страницы переписок">
              <button disabled={page === 0} onClick={() => setPage((value) => value - 1)}>
                Назад
              </button>
              <span>{page + 1}</span>
              <button disabled={!hasMore} onClick={() => setPage((value) => value + 1)}>
                Дальше
              </button>
            </nav>
          )}
        </aside>
        <div className="admin-chat-detail">
          {current ? (
            <>
              <header className="chat-panel-heading ws-chat-head">
                <button
                  className="admin-chat-back"
                  type="button"
                  aria-label="К перепискам"
                  onClick={() => setSelected(null)}
                >
                  <svg
                    viewBox="0 0 24 24"
                    width="22"
                    height="22"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    aria-hidden="true"
                  >
                    <path d="m15 5-7 7 7 7" />
                  </svg>
                </button>
                <Avatar item={current} size="small" />
                <div>
                  <h2>{current.name}</h2>
                  <p>{current.status === "closed" ? "Вопрос решён" : `Обращение №${current.id}`}</p>
                </div>
                {current.order && (
                  <a
                    className="ws-chat-order"
                    href={`/admin?section=orders&order=${current.order.id}`}
                  >
                    Заказ {current.order.number} ·{" "}
                    {statusLabels[current.order.status] || current.order.status}
                  </a>
                )}
                <button
                  className="chat-resolve"
                  data-closed={current.status === "closed"}
                  disabled={changing}
                  type="button"
                  onClick={() => void toggleStatus()}
                >
                  {changing
                    ? "Сохраняем…"
                    : current.status === "closed"
                      ? "Открыть снова"
                      : "Вопрос решён"}
                </button>
              </header>
              <ChatThread key={current.id} threadId={current.id} onUpdate={() => void refresh()} />
            </>
          ) : (
            <div className="chat-welcome">
              <h2>Выберите переписку</h2>
              <p>Вопросы о растениях, фотографии и заказы покупателей — здесь.</p>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
