import { useCallback, useEffect, useRef, useState } from "react";
import ChatThread from "./ChatThread";
import { apiJSON } from "./lib/api";
import { chatRequest } from "./lib/chat-api";

type Summary = { id: number; name: string; status: "open" | "closed"; preview: string; unread: number; updatedAt: string };
export default function AdminChat() {
  const [items, setItems] = useState<Summary[]>([]);
  const [selected, setSelected] = useState<Summary | null>(null);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("open");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [hasMore, setHasMore] = useState(false);
  const [page, setPage] = useState(0);
  const [changing, setChanging] = useState(false);
  const requestNumber = useRef(0);
  const refresh = useCallback(async () => {
    const number = ++requestNumber.current;
    try {
      const data = await apiJSON<{ items: Summary[]; hasMore: boolean }>(`/api/v1/admin/chat?${new URLSearchParams({ q: query, status, offset: String(page * 50) })}`);
      if (number !== requestNumber.current) return;
      setItems(data.items); setHasMore(data.hasMore); setError("");
    } catch (caught) { if (number === requestNumber.current) setError((caught as Error).message); }
    finally { if (number === requestNumber.current) setLoading(false); }
  }, [query, status, page]);
  useEffect(() => {
    const requests = requestNumber;
    const debounce = window.setTimeout(() => void refresh(), 250);
    const timer = window.setInterval(() => { if (document.visibilityState === "visible") void refresh(); }, 5000);
    window.addEventListener("focus", refresh);
    return () => { requests.current++; window.clearTimeout(debounce); window.clearInterval(timer); window.removeEventListener("focus", refresh); };
  }, [refresh]);
  const toggleStatus = async () => {
    if (!selected || changing) return;
    setChanging(true);
    try { const next = (items.find(item => item.id === selected.id) || selected).status === "closed" ? "open" : "closed"; await chatRequest(`/api/v1/admin/chat/${selected.id}`, { status: next }, "PATCH"); setSelected({ ...selected, status: next }); await refresh(); }
    catch (caught) { setError((caught as Error).message); } finally { setChanging(false); }
  };
  const current = items.find(item => item.id === selected?.id) || selected;
  return <section className="admin-chat telegram-chat"><header className="admin-chat-title"><h1>Чат с покупателями</h1><p>Сообщения с сайта. Ответы остаются в личной переписке покупателя.</p></header>{error && <div className="chat-error" role="alert">{error}<button onClick={() => void refresh()}>Повторить</button></div>}<div className={`admin-chat-layout ${selected ? "admin-chat-selected" : ""}`}><aside className="admin-chat-inbox" aria-label="Обращения покупателей"><div className="admin-chat-filters"><label>Найти переписку<input value={query} maxLength={100} placeholder="Имя, номер или текст" onChange={event => { setQuery(event.target.value); setPage(0); }} /></label><label>Показать<select value={status} onChange={event => { setStatus(event.target.value); setPage(0); }}><option value="open">Открытые</option><option value="closed">Решённые</option><option value="">Все обращения</option></select></label></div><div className="admin-chat-list">{loading && <p className="chat-state" role="status">Загружаем обращения…</p>}{!loading && !items.length && <p className="chat-state">{query ? "Переписок по этому запросу нет." : "Здесь появятся обращения покупателей."}</p>}{items.map(item => <button type="button" className={`admin-chat-item ${selected?.id === item.id ? "active" : ""}`} key={item.id} aria-pressed={selected?.id === item.id} onClick={() => setSelected(item)}><span><strong>{item.name}</strong><time dateTime={item.updatedAt}>{new Date(item.updatedAt).toLocaleDateString("ru-RU", { day: "numeric", month: "short" })}</time></span><p>{item.preview}</p><small>{item.status === "closed" ? "Решён" : "Открыт"}{item.unread > 0 && <b>{item.unread} новых</b>}</small></button>)}</div>{(page > 0 || hasMore) && <nav className="admin-chat-pages" aria-label="Страницы обращений"><button disabled={page === 0} onClick={() => setPage(value => value - 1)}>Назад</button><span>{page + 1}</span><button disabled={!hasMore} onClick={() => setPage(value => value + 1)}>Дальше</button></nav>}</aside><div className="admin-chat-detail">{current ? <><header className="chat-panel-heading"><button className="admin-chat-back" type="button" onClick={() => setSelected(null)}>К обращениям</button><div><h2>{current.name}</h2><p>Обращение №{current.id}</p></div><button className="chat-resolve" disabled={changing} type="button" onClick={() => void toggleStatus()}>{changing ? "Сохраняем…" : current.status === "closed" ? "Открыть снова" : "Вопрос решён"}</button></header><ChatThread key={current.id} threadId={current.id} onUpdate={() => void refresh()} /></> : <div className="chat-welcome"><h2>Выберите переписку</h2><p>Вопросы о растениях, фотографии и история общения будут здесь.</p></div>}</div></div></section>;
}
