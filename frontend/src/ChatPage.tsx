import { useEffect } from "react";
import { StoreHeader } from "./StoreHeader";
import ChatThread from "./ChatThread";
import type { ChatContext } from "./lib/chat";

export default function ChatPage() {
  const params = new URLSearchParams(window.location.search);
  const url = params.get("from") || "";
  const label = params.get("topic") || "";
  const context: ChatContext | undefined = /^\/(product|care|account\/orders)\//.test(url) && !/[\\\r\n]/.test(url) ? { url: url.slice(0, 500), label: label.slice(0, 180) || "Вопрос по странице" } : undefined;
  useEffect(() => { document.title = "Чат с Фикусином — помощь с растениями и заказами"; }, []);
  return <main className="chat-page"><StoreHeader /><div className="chat-layout"><aside className="chat-intro"><a href="/" className="chat-back">Вернуться в магазин</a><h1>На связи,<br />Фикусин</h1><p>Поможем выбрать растение, разобраться с уходом или уточнить детали заказа.</p><div className="chat-intro-note"><strong>Отвечает сотрудник магазина</strong><p>Можно оставить сообщение в любое время. Ответ появится в этой переписке, когда сотрудник его прочитает и ответит.</p></div><div className="chat-intro-note"><strong>Расскажите чуть подробнее</strong><p>О растении — где стоит и как поливаете. О заказе — его номер. Фотография поможет быстрее разобраться.</p></div><p className="chat-storage-note">Без входа история доступна в этом браузере. <a href="/login?returnTo=%2Fchat">Войдите в аккаунт</a>, чтобы продолжить на другом устройстве.</p><a className="chat-phone" href="tel:+79156151100">+7 915 615-11-00<span>Позвонить в магазин</span></a></aside><section className="chat-panel" aria-label="Чат с Фикусином"><header className="chat-panel-heading"><div className="chat-monogram" aria-hidden="true">Ф</div><div><h2>Фикусин</h2><p>Вопросы о растениях и заказах</p></div></header><ChatThread context={context} /></section></div></main>;
}
