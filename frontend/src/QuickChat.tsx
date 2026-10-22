import { useEffect, useRef } from "react";
import ChatThread, { ChatIcon } from "./ChatThread";
import type { ChatContext } from "./lib/chat";

export default function QuickChat({ open, context, onClose }: { open: boolean; context?: ChatContext; onClose: () => void }) {
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => { if (open) close.current?.focus(); }, [open]);
  return <section id="quick-chat" className="quick-chat telegram-chat" hidden={!open} role="dialog" aria-label="Быстрый чат с Фикусином" onKeyDown={event => { if (event.key === "Escape") { event.preventDefault(); onClose(); } }}><header className="chat-panel-heading"><div className="chat-monogram" aria-hidden="true">Ф</div><div><h2>Фикусин</h2><p>Сотрудник магазина</p></div><a className="chat-header-action" href="/account/chat" aria-label="Вся переписка в личном кабинете" title="Открыть в личном кабинете"><ChatIcon name="expand" /></a><button ref={close} className="chat-header-action" type="button" aria-label="Свернуть чат" onClick={onClose}><ChatIcon name="close" /></button></header><div className="quick-chat-account-note"><span>История переписки — в </span><a href="/account/chat">личном кабинете</a></div><ChatThread context={context} visible={open} /></section>;
}
