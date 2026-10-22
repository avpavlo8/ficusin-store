import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { apiJSON } from "./lib/api";
import { chatPhoto } from "./lib/chat-photo";
import { chatRequest } from "./lib/chat-api";
import type { ChatContext, ChatThreadData } from "./lib/chat";
import "./styles/chat.css";

export function ChatIcon({ name }: { name: "chat" | "send" | "photo" | "close" }) {
  const paths = { chat: "M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-6 4V6a2 2 0 0 1 2-2ZM7 9h10M7 13h6", send: "m3 3 18 9-18 9 4-9-4-9Zm4 9h14", photo: "M4 4h16v16H4ZM4 16l5-5 4 4 3-3 4 4M15 8h.01", close: "m6 6 12 12M6 18 18 6" };
  return <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[name]} /></svg>;
}
const empty: ChatThreadData = { id: 0, status: "open", customerReadId: 0, staffReadId: 0, messages: [], hasMore: false };
const moment = (date: string) => new Date(date).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
const day = (date: string) => new Date(date).toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric" });

export default function ChatThread({ threadId, context, onUpdate }: { threadId?: number; context?: ChatContext; onUpdate?: () => void }) {
  const staff = threadId !== undefined;
  const base = staff ? `/api/v1/admin/chat/${threadId}` : "/api/v1/chat";
  const [thread, setThread] = useState<ChatThreadData>(empty);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [sending, setSending] = useState(false);
  const [preparing, setPreparing] = useState(false);
  const [older, setOlder] = useState(false);
  const [text, setText] = useState("");
  const [image, setImage] = useState("");
  const [consent, setConsent] = useState(false);
  const [topic, setTopic] = useState(context);
  const [newCount, setNewCount] = useState(0);
  const scroll = useRef<HTMLDivElement>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const pending = useRef({ signature: "", clientId: "" });
  const latest = useRef(empty);
  const nearBottom = useRef(true);
  const readThrough = useRef(0);
  const active = useRef(true);
  const onUpdateRef = useRef(onUpdate);
  useEffect(() => { onUpdateRef.current = onUpdate; }, [onUpdate]);

  const accept = useCallback((data: ChatThreadData) => {
    const previous = latest.current;
    const hasNew = data.messages.some(m => m.id > (previous.messages.at(-1)?.id || 0));
    const merged = new Map(previous.messages.map(m => [m.id, m]));
    data.messages.forEach(m => merged.set(m.id, m));
    const next = { ...data, customerReadId: Math.max(data.customerReadId, previous.customerReadId), staffReadId: Math.max(data.staffReadId, previous.staffReadId), hasMore: previous.messages.length ? previous.hasMore : data.hasMore, messages: [...merged.values()].sort((a, b) => a.id - b.id) };
    latest.current = next;
    setThread(next);
    if (hasNew) {
      if (nearBottom.current) requestAnimationFrame(() => { const el = scroll.current; if (el) el.scrollTop = el.scrollHeight; });
      else setNewCount(count => count + data.messages.filter(m => m.id > (previous.messages.at(-1)?.id || 0)).length);
    }
  }, []);
  const load = useCallback(async () => {
    try {
      const data = await apiJSON<ChatThreadData>(base);
      if (active.current) { accept(data); setLoadError(""); }
    } catch (caught) { if (active.current) setLoadError((caught as Error).message); }
    finally { if (active.current) setLoading(false); }
  }, [base, accept]);
  useEffect(() => {
    active.current = true;
    let stopped = false;
    let timer: number;
    const poll = async () => { if (document.visibilityState === "visible") await load(); if (!stopped) timer = window.setTimeout(poll, 4000); };
    void poll();
    const refresh = () => { if (document.visibilityState === "visible") void load(); };
    window.addEventListener("focus", refresh); document.addEventListener("visibilitychange", refresh);
    return () => { active.current = false; stopped = true; window.clearTimeout(timer); window.removeEventListener("focus", refresh); document.removeEventListener("visibilitychange", refresh); };
  }, [load]);
  const markRead = useCallback(() => {
    if (document.visibilityState !== "visible" || !nearBottom.current) return;
    const data = latest.current;
    const last = data.messages.at(-1)?.id || 0;
    if (!data.id || last <= readThrough.current) return;
    readThrough.current = last;
    void chatRequest(base + "/read", { through: last }).then(() => { window.dispatchEvent(new Event("ficusin-chat-read")); onUpdateRef.current?.(); }).catch(() => { readThrough.current = 0; });
  }, [base]);
  useEffect(() => { markRead(); document.addEventListener("visibilitychange", markRead); return () => document.removeEventListener("visibilitychange", markRead); }, [thread, markRead]);

  const send = async (event: FormEvent) => {
    event.preventDefault(); if (sending || preparing || loading || (!text.trim() && !image)) return;
    setSending(true); setError("");
    const signature = JSON.stringify([text.trim(), image, topic]);
    if (pending.current.signature !== signature) pending.current = { signature, clientId: crypto.randomUUID() };
    try {
      const data = await chatRequest<ChatThreadData>(base + "/messages", { clientId: pending.current.clientId, text: text.trim(), image, consent, contextUrl: topic?.url || "", contextLabel: topic?.label || "" });
      if (!active.current) return;
      nearBottom.current = true; accept(data); setText(""); setImage(""); setTopic(undefined); pending.current = { signature: "", clientId: "" }; onUpdateRef.current?.(); textarea.current?.focus();
    } catch (caught) { if (active.current) setError(`${(caught as Error).message}. Текст сохранён — можно отправить повторно.`); }
    finally { if (active.current) setSending(false); }
  };
  const loadOlder = async () => {
    setOlder(true);
    try {
      const el = scroll.current; const height = el?.scrollHeight || 0; const top = el?.scrollTop || 0;
      const data = await apiJSON<ChatThreadData>(base + `?before=${thread.messages[0]?.id || 0}`);
      if (!active.current) return;
      const merged = new Map([...data.messages, ...latest.current.messages].map(m => [m.id, m]));
      const next = { ...latest.current, hasMore: data.hasMore, messages: [...merged.values()].sort((a, b) => a.id - b.id) };
      latest.current = next; setThread(next);
      requestAnimationFrame(() => { if (el) el.scrollTop = top + el.scrollHeight - height; });
    } catch (caught) { if (active.current) setError((caught as Error).message); }
    finally { if (active.current) setOlder(false); }
  };
  return <div className="chat-thread">
    <div className="chat-messages" ref={scroll} role="log" aria-label="Переписка с магазином" aria-live="polite" aria-relevant="additions" tabIndex={0} onScroll={() => { const el = scroll.current; if (el) { nearBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 70; if (nearBottom.current) { setNewCount(0); markRead(); } } }}>
      {loading && <p className="chat-state" role="status">Загружаем переписку…</p>}
      {!loading && !thread.messages.length && <div className="chat-welcome"><ChatIcon name="chat" /><h2>{staff ? "Начало переписки" : "Растения, заказы и добрый совет"}</h2><p>{staff ? "Здесь появятся сообщения покупателя." : "Напишите, чем помочь. Сотрудник магазина ответит здесь. Для вопроса об уходе можно прикрепить фото."}</p></div>}
      {thread.hasMore && <button type="button" className="chat-history" disabled={older} onClick={() => void loadOlder()}>{older ? "Загружаем…" : "Предыдущие сообщения"}</button>}
      {thread.messages.map((message, index) => {
        const own = message.sender === (staff ? "staff" : "customer");
        const read = message.id <= (staff ? thread.customerReadId : thread.staffReadId);
        return <div className="chat-message-group" key={message.id}>
          {(index === 0 || day(thread.messages[index - 1].createdAt) !== day(message.createdAt)) && <p className="chat-date">{day(message.createdAt)}</p>}
          <article className={`chat-message ${own ? "chat-message-own" : "chat-message-other"}`} aria-label={`${message.sender === "staff" ? "Фикусин" : "Покупатель"}, ${moment(message.createdAt)}`}>
            <span className="chat-sender">{message.sender === "staff" ? "Фикусин" : own ? "Вы" : "Покупатель"}</span>
            {message.contextUrl && <a className="chat-context-link" href={message.contextUrl}>{message.contextLabel || "Страница обращения"}</a>}
            {message.hasImage && <a className="chat-image-link" href={`${base}/photos/${message.id}`} target="_blank" rel="noreferrer"><img src={`${base}/photos/${message.id}`} alt="Фотография в переписке — открыть целиком" loading="lazy" onLoad={() => { if (nearBottom.current && scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight; }} /></a>}
            {message.text && <p>{message.text}</p>}
            <div className="chat-message-meta"><time dateTime={message.createdAt}>{moment(message.createdAt)}</time>{own && <span>{read ? "Прочитано" : "Отправлено"}</span>}</div>
          </article>
        </div>;
      })}
      {!loading && thread.status === "closed" && <p className="chat-state">Вопрос решён. Новое сообщение снова откроет переписку.</p>}
    </div>
    {newCount > 0 && <button type="button" className="chat-new" onClick={() => { if (scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight; nearBottom.current = true; setNewCount(0); markRead(); }}>Новые сообщения: {newCount}</button>}
    {(error || loadError) && <div className="chat-error" role="alert"><span>{error || loadError}</span><button type="button" onClick={() => void load()}>Обновить</button></div>}
    <form className="chat-compose" onSubmit={event => void send(event)}>
      {topic && <div className="chat-topic"><span>Обсуждаем: <a href={topic.url}>{topic.label}</a></span><button type="button" aria-label="Убрать тему обращения" disabled={sending} onClick={() => setTopic(undefined)}><ChatIcon name="close" /></button></div>}
      {image && <div className="chat-preview"><img src={image} alt="Фото к отправке" /><button type="button" disabled={sending} onClick={() => setImage("")}>Убрать фото</button></div>}
      <label className="chat-sr-only" htmlFor={`chat-text-${threadId || "customer"}`}>Сообщение</label>
      <textarea ref={textarea} id={`chat-text-${threadId || "customer"}`} value={text} maxLength={4000} disabled={sending} placeholder={staff ? "Ответ покупателю…" : "Напишите ваше сообщение…"} rows={3} onChange={event => setText(event.target.value)} onKeyDown={event => { if (event.key === "Enter" && (event.ctrlKey || event.metaKey) && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} />
      {!staff && !thread.id && !loading && <label className="chat-consent"><input type="checkbox" checked={consent} disabled={sending} onChange={event => setConsent(event.target.checked)} /><span>Согласен на обработку данных из переписки по <a href="/privacy" target="_blank" rel="noreferrer">политике конфиденциальности</a>.</span></label>}
      <div className="chat-compose-actions"><label className={`chat-attach ${sending || preparing ? "chat-attach-disabled" : ""}`}><ChatIcon name="photo" /><span>{preparing ? "Готовим фото…" : "Добавить фото"}</span><input className="chat-sr-only" type="file" accept="image/*" disabled={sending || preparing} aria-label="Добавить фотографию" onChange={event => { const file = event.target.files?.[0]; event.target.value = ""; if (!file) return; setPreparing(true); setError(""); void chatPhoto(file).then(data => { if (active.current) setImage(data); }).catch(caught => { if (active.current) setError((caught as Error).message); }).finally(() => { if (active.current) setPreparing(false); }); }} /></label><small>{text.length}/4000</small><button className="primary-button" type="submit" disabled={loading || sending || preparing || (!text.trim() && !image) || (!staff && !thread.id && !consent)}><span>{sending ? "Отправляем…" : "Отправить"}</span><ChatIcon name="send" /></button></div>
    </form>
  </div>;
}
