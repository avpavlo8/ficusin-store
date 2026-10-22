import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type FormEvent } from "react";
import { apiJSON } from "./lib/api";
import { chatFileSize, prepareChatAttachment, type PreparedAttachment } from "./lib/chat-attachment";
import { chatRequest } from "./lib/chat-api";
import ChatPhotoViewer from "./ChatPhotoViewer";
import type { ChatContext, ChatThreadData } from "./lib/chat";
import "./styles/chat.css";

export function ChatIcon({ name }: { name: "chat" | "send" | "photo" | "close" | "read" | "sent" | "clip" | "smile" | "file" }) {
  const paths = { clip: "m8 12 6-6a3 3 0 0 1 4 4l-8 8a5 5 0 0 1-7-7l9-9m-6 9-1 1a1.5 1.5 0 0 0 2 2l7-7", smile: "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM8 9h.01M16 9h.01M8 14a4 4 0 0 0 8 0", file: "M14 3H5v18h14V8Zm0 0v5h5M8 12h8M8 16h6", read: "m2 12 4 4 8-8m-3 8 8-8", sent: "m5 12 4 4 10-10", chat: "M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-6 4V6a2 2 0 0 1 2-2ZM7 9h10M7 13h6", send: "m3 3 18 9-18 9 4-9-4-9Zm4 9h14", photo: "M4 4h16v16H4ZM4 16l5-5 4 4 3-3 4 4M15 8h.01", close: "m6 6 12 12M6 18 18 6" };
  return <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[name]} /></svg>;
}
function contextHref(value: string): string | undefined {
  const parts = value.split("/");
  try {
    if (parts[1] === "product") return `/product/${encodeURIComponent(decodeURIComponent(parts.slice(2).join("/")))}`;
    if (parts[1] === "care") return `/care/${encodeURIComponent(decodeURIComponent(parts.slice(2).join("/")))}`;
    if (parts[1] === "account" && parts[2] === "orders") return `/account/orders/${encodeURIComponent(decodeURIComponent(parts.slice(3).join("/")))}`;
  } catch { return undefined; }
  return undefined;
}
const empty: ChatThreadData = { id: 0, status: "open", customerReadId: 0, staffReadId: 0, messages: [], hasMore: false, consentRequired: true };
const moment = (date: string) => new Date(date).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
const day = (date: string) => new Date(date).toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric" });

export default function ChatThread({ threadId, context, onUpdate, visible = true }: { threadId?: number; context?: ChatContext; onUpdate?: () => void; visible?: boolean }) {
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
  const [attachment, setAttachment] = useState<PreparedAttachment | null>(null);
  const [emojiOpen, setEmojiOpen] = useState(false);
  const [photo, setPhoto] = useState<string | null>(null);
  const photoTrigger = useRef<HTMLButtonElement>(null);
  const inputID = useId();
  const fileInput = useRef<HTMLInputElement>(null);
  const [consent, setConsent] = useState(false);
  const [topic, setTopic] = useState(context);
  const [newCount, setNewCount] = useState(0);
  useEffect(() => { const timer = window.setTimeout(() => setTopic(context), 0); return () => window.clearTimeout(timer); }, [context]);
  const scroll = useRef<HTMLDivElement>(null);
  const compose = useRef<HTMLFormElement>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const form = compose.current;
    const frame = scroll.current?.parentElement;
    if (!form || !frame) return;
    const resize = () => form.style.setProperty('--chat-emoji-max-height', `${Math.max(0, frame.clientHeight - form.offsetHeight - 12)}px`);
    const observer = new ResizeObserver(resize);
    observer.observe(form); observer.observe(frame); resize();
    return () => observer.disconnect();
  }, []);
  useLayoutEffect(() => { const field = textarea.current; if (field) { field.style.height = "auto"; field.style.height = `${Math.min(120, Math.max(36, field.scrollHeight))}px`; } }, [text]);
  const pending = useRef<{ signature: string; clientId: string; attachment: PreparedAttachment | null }>({ signature: "", clientId: "", attachment: null });
  const latest = useRef(empty);
  const nearBottom = useRef(true);
  const readThrough = useRef(0);
  const active = useRef(true);
  const onUpdateRef = useRef(onUpdate);
  const visibleRef = useRef(visible);
  useEffect(() => { visibleRef.current = visible; }, [visible]);
  useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);
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
      let after = latest.current.messages.at(-1)?.id || 0;
      let more = true;
      while (more && active.current && visibleRef.current) {
        const data = await apiJSON<ChatThreadData>(base + (after ? `?after=${after}` : ""));
        if (!active.current) return;
        more = after > 0 && data.hasMore;
        if (after) data.hasMore = latest.current.hasMore;
        accept(data); setLoadError("");
        const next = data.messages.at(-1)?.id || 0;
        if (!next || next <= after) break;
        after = next;
      }
    } catch (caught) { if (active.current) setLoadError((caught as Error).message); }
    finally { if (active.current) setLoading(false); }
  }, [base, accept]);
  useEffect(() => {
    if (!visible) return;
    let stopped = false;
    let timer: number;
    const poll = async () => { if (document.visibilityState === "visible") await load(); if (!stopped) timer = window.setTimeout(poll, 4000); };
    void poll();
    const refresh = () => { if (document.visibilityState === "visible") void load(); };
    window.addEventListener("focus", refresh); document.addEventListener("visibilitychange", refresh);
    return () => { stopped = true; window.clearTimeout(timer); window.removeEventListener("focus", refresh); document.removeEventListener("visibilitychange", refresh); };
  }, [load, visible]);
  const markRead = useCallback(() => {
    if (!visibleRef.current || document.visibilityState !== "visible" || !scroll.current) return;
    const panel = scroll.current.getBoundingClientRect();
    const viewport = window.visualViewport;
    const top = Math.max(panel.top, viewport?.offsetTop || 0, 0);
    const bottom = Math.min(panel.bottom, (viewport?.offsetTop || 0) + (viewport?.height || window.innerHeight));
    let through = 0;
    scroll.current.querySelectorAll<HTMLElement>('[data-chat-incoming="true"]').forEach(node => {
      const box = node.getBoundingClientRect();
      if (box.width > 0 && box.height > 0 && box.bottom > top && box.bottom <= bottom + 1 && box.top < bottom) through = Math.max(through, Number(node.dataset.messageId));
    });
    if (!latest.current.id || through <= readThrough.current) return;
    readThrough.current = through;
    void chatRequest(base + "/read", { through }).then(() => { window.dispatchEvent(new Event("ficusin-chat-read")); onUpdateRef.current?.(); }).catch(() => { readThrough.current = 0; });
  }, [base]);
  useEffect(() => {
    markRead();
    const observer = new IntersectionObserver(markRead, { threshold: [0, .25, 1] });
    scroll.current?.querySelectorAll('[data-chat-incoming="true"]').forEach(node => observer.observe(node));
    document.addEventListener("visibilitychange", markRead); window.addEventListener("scroll", markRead, true); window.addEventListener("resize", markRead); window.visualViewport?.addEventListener("resize", markRead);
    return () => { observer.disconnect(); document.removeEventListener("visibilitychange", markRead); window.removeEventListener("scroll", markRead, true); window.removeEventListener("resize", markRead); window.visualViewport?.removeEventListener("resize", markRead); };
  }, [thread, markRead, visible]);

  const send = async (event: FormEvent) => {
    event.preventDefault(); if (sending || preparing || loading || (!text.trim() && !attachment)) return;
    setSending(true); setError("");
    const signature = JSON.stringify([text.trim(), topic]);
    if (pending.current.signature !== signature || pending.current.attachment !== attachment) pending.current = { signature, clientId: crypto.randomUUID(), attachment };
    try {
      const data = await chatRequest<ChatThreadData>(base + "/messages", { clientId: pending.current.clientId, text: text.trim(), attachment: attachment ? { name: attachment.name, data: attachment.data } : undefined, consent, contextUrl: topic?.url || "", contextLabel: topic?.label || "" });
      if (!active.current) return;
      nearBottom.current = true; if (latest.current.messages.length) await load(); else accept(data); setText(""); setAttachment(null); setTopic(undefined); pending.current = { signature: "", clientId: "", attachment: null }; onUpdateRef.current?.(); textarea.current?.focus();
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
  return <div className="chat-thread" onKeyDown={event => { if (emojiOpen && event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setEmojiOpen(false); textarea.current?.focus(); } }}>
    <div className="chat-messages" ref={scroll} role="log" aria-label="Переписка с магазином" aria-live="polite" aria-relevant="additions" tabIndex={0} onScroll={() => { const el = scroll.current; if (el) { nearBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 70; if (nearBottom.current) { setNewCount(0); markRead(); } } }}>
      {loading && <p className="chat-state" role="status">Загружаем переписку…</p>}
      {!loading && !thread.messages.length && <div className="chat-welcome"><ChatIcon name="chat" /><h2>{staff ? "Начало переписки" : "Растения, заказы и добрый совет"}</h2><p>{staff ? "Здесь появятся сообщения покупателя." : "Напишите, чем помочь. Сотрудник магазина ответит здесь. Для вопроса об уходе можно прикрепить фото."}</p></div>}
      {thread.hasMore && <button type="button" className="chat-history" disabled={older} onClick={() => void loadOlder()}>{older ? "Загружаем…" : "Предыдущие сообщения"}</button>}
      {thread.messages.map((message, index) => {
        const own = message.sender === (staff ? "staff" : "customer");
        const read = message.id <= (staff ? thread.customerReadId : thread.staffReadId);
        return <div className="chat-message-group" key={message.id}>
          {(index === 0 || day(thread.messages[index - 1].createdAt) !== day(message.createdAt)) && <p className="chat-date">{day(message.createdAt)}</p>}
          <article data-message-id={message.id} data-chat-incoming={!own} className={`chat-message ${own ? "chat-message-own" : "chat-message-other"}`} aria-label={`${message.sender === "staff" ? "Фикусин" : "Покупатель"}, ${moment(message.createdAt)}`}>
            {message.contextUrl && <a className="chat-context-link" href={contextHref(message.contextUrl)}>{message.contextLabel || "Страница обращения"}</a>}
            {message.hasImage && <button type="button" className="chat-image-link" aria-label="Открыть фотографию" onClick={event => { photoTrigger.current = event.currentTarget; setEmojiOpen(false); setPhoto(`${base}/photos/${message.id}`); }}><img src={`${base}/photos/${message.id}`} alt="Фотография в переписке — открыть целиком" loading="lazy" onLoad={() => { if (nearBottom.current && scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight; }} /></button>}
            {message.attachmentKind === "video" && <div className="chat-video"><video controls playsInline preload="metadata" src={`${base}/attachments/${message.id}`} aria-label={message.attachmentName || "Видео в переписке"} /><a href={`${base}/attachments/${message.id}`} download>{message.attachmentName || "Открыть видео"}</a></div>}
            {message.attachmentKind === "file" && <a className="chat-file-link" href={`${base}/attachments/${message.id}`} download><ChatIcon name="file" /><span><strong>{message.attachmentName || "Файл"}</strong><small>{chatFileSize(message.attachmentSize || 0)} · Скачать</small></span></a>}
            {message.text && <p>{message.text}</p>}
            <div className="chat-message-meta"><time dateTime={message.createdAt}>{moment(message.createdAt)}</time>{own && <span className="chat-receipt" aria-label={read ? "Прочитано" : "Отправлено"}><ChatIcon name={read ? "read" : "sent"} /><span className="chat-sr-only">{read ? "Прочитано" : "Отправлено"}</span></span>}</div>
          </article>
        </div>;
      })}
      {!loading && thread.status === "closed" && <p className="chat-state">Вопрос решён. Новое сообщение снова откроет переписку.</p>}
    </div>
    {newCount > 0 && <button type="button" className="chat-new" onClick={() => { if (scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight; nearBottom.current = true; setNewCount(0); markRead(); }}>Новые сообщения: {newCount}</button>}
    {(error || loadError) && <div className="chat-error" role="alert"><span>{error || loadError}</span><button type="button" onClick={() => void load()}>Обновить</button></div>}
    <form ref={compose} className="chat-compose" onSubmit={event => void send(event)}>
      {topic && <div className="chat-topic"><span>Обсуждаем: <a href={contextHref(topic.url)}>{topic.label}</a></span><button type="button" aria-label="Убрать тему обращения" disabled={sending} onClick={() => setTopic(undefined)}><ChatIcon name="close" /></button></div>}
      {attachment && <div className="chat-preview">{attachment.preview ? <img src={attachment.preview} alt="Фото к отправке" /> : <ChatIcon name={attachment.kind === "video" ? "photo" : "file"} />}<div><strong>{attachment.name}</strong><small>{chatFileSize(attachment.size)}</small></div><button type="button" disabled={sending} aria-label="Убрать вложение" onClick={() => setAttachment(null)}><ChatIcon name="close" /></button></div>}
      {emojiOpen && <div className="chat-emoji-panel" role="group" aria-label="Выбор смайлика" onKeyDown={event => { if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setEmojiOpen(false); textarea.current?.focus(); } }}>
        {["😀","😊","🙂","😉","😍","🥰","😂","😅","🤔","😔","😢","😮","👍","👎","🙏","🤝","❤️","💚","✨","🌿","🌱","🪴","🌸","☀️","💧","📦","✅","❓","🙌","🎉","👋","🤗"].map(emoji => <button type="button" key={emoji} aria-label={`Вставить ${emoji}`} onClick={() => { const field = textarea.current; const start = field?.selectionStart ?? text.length; const end = field?.selectionEnd ?? start; const next = text.slice(0, start) + emoji + text.slice(end); if (next.length > 4000) return; setText(next); setEmojiOpen(false); requestAnimationFrame(() => { field?.focus(); field?.setSelectionRange(start + emoji.length, start + emoji.length); }); }}>{emoji}</button>)}
      </div>}
      <div className="chat-composer-bar">
        <button className="chat-compose-icon" type="button" disabled={sending || preparing} aria-label="Прикрепить фото, видео или файл" title="Фото, видео или файл до 20 МБ" onClick={() => fileInput.current?.click()}><ChatIcon name="clip" /></button>
        <input ref={fileInput} className="chat-sr-only" tabIndex={-1} type="file" disabled={sending || preparing} aria-label="Добавить фото, видео или файл" onChange={event => { const file = event.target.files?.[0]; event.target.value = ""; if (!file) return; setPreparing(true); setError(""); void prepareChatAttachment(file).then(data => { if (active.current) setAttachment(data); }).catch(caught => { if (active.current) setError((caught as Error).message); }).finally(() => { if (active.current) setPreparing(false); }); }} />
        <label className="chat-sr-only" htmlFor={inputID}>Сообщение</label>
        <textarea ref={textarea} id={inputID} value={text} maxLength={4000} disabled={sending} placeholder={preparing ? "Готовим вложение…" : staff ? "Сообщение покупателю…" : "Сообщение…"} rows={1} onChange={event => setText(event.target.value)} onKeyDown={event => { if (event.key === "Enter" && (event.ctrlKey || event.metaKey) && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} />
        <button className="chat-compose-icon" type="button" aria-label="Смайлики" aria-expanded={emojiOpen} disabled={sending} onClick={() => setEmojiOpen(value => !value)}><ChatIcon name="smile" /></button>
        <button className="chat-send" type="submit" aria-label={sending ? "Отправляем…" : "Отправить"} disabled={loading || sending || preparing || (!text.trim() && !attachment) || (!staff && !thread.id && thread.consentRequired && !consent)}><ChatIcon name="send" /></button>
      </div>
      {!staff && !thread.id && !loading && thread.consentRequired && <label className="chat-consent"><input type="checkbox" checked={consent} disabled={sending} onChange={event => setConsent(event.target.checked)} /><span>Согласен на обработку данных из переписки по <a href="/privacy" target="_blank" rel="noreferrer">политике конфиденциальности</a>.</span></label>}

    </form>
    {photo && <ChatPhotoViewer src={photo} onClose={() => { setPhoto(null); requestAnimationFrame(() => photoTrigger.current?.focus({ preventScroll: true })); }} />}
  </div>;
}
