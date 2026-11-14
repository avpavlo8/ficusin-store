import { useEffect, useRef, useState } from "react";
import { apiJSON } from "./lib/api";
import type { ChatReference } from "./lib/chat";
import { chatOrderStatus } from "./lib/chat-reference";
export function ChatReferencePicker({
  base,
  kind,
  onSelect,
  onClose,
}: {
  base: string;
  kind: "product" | "order";
  onSelect: (reference: ChatReference) => void;
  onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const [state, setState] = useState<{
    items: ChatReference[];
    busy: boolean;
    error: string;
    loginRequired?: boolean;
  }>({ items: [], busy: true, error: "" });
  const search = useRef<HTMLInputElement>(null);
  useEffect(() => {
    search.current?.focus({ preventScroll: true });
  }, []);
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      setState((current) => ({ ...current, busy: true, error: "" }));
      apiJSON<{ items: ChatReference[]; loginRequired?: boolean }>(
        `${base}/references?kind=${kind}&q=${encodeURIComponent(query)}`,
        { signal: controller.signal },
      )
        .then((data) => {
          if (active) setState({ ...data, busy: false, error: "" });
        })
        .catch((error) => {
          if (active) setState({ items: [], busy: false, error: (error as Error).message });
        });
    }, 200);
    return () => {
      active = false;
      clearTimeout(timer);
      controller.abort();
    };
  }, [base, kind, query]);
  return (
    <div
      className="chat-reference-picker"
      role="dialog"
      aria-label={kind === "product" ? "Прикрепить товар" : "Прикрепить заказ"}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          onClose();
        }
      }}
    >
      <div className="chat-reference-heading">
        <strong>{kind === "product" ? "Выберите товар" : "Выберите заказ"}</strong>
        <button type="button" aria-label="Закрыть выбор вложения" onClick={onClose}>
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            aria-hidden="true"
          >
            <path d="m6 6 12 12M6 18 18 6" />
          </svg>
        </button>
      </div>
      <label>
        <span className="chat-sr-only">{kind === "product" ? "Поиск товара" : "Поиск заказа"}</span>
        <input
          ref={search}
          type="search"
          value={query}
          placeholder={
            kind === "product" ? "Название или артикул" : "Номер заказа или название товара"
          }
          onChange={(event) => setQuery(event.target.value)}
        />
      </label>
      <div className="chat-reference-options" aria-busy={state.busy}>
        {state.busy ? (
          <p role="status">Загружаем…</p>
        ) : state.error ? (
          <p role="alert">{state.error}</p>
        ) : state.loginRequired ? (
          <p>
            Чтобы прикрепить свой заказ,{" "}
            <a href={`/login?returnTo=${encodeURIComponent(location.pathname + location.search)}`}>
              войдите в аккаунт
            </a>
            .
          </p>
        ) : !state.items.length ? (
          <p>
            {query
              ? "Ничего не найдено"
              : kind === "order"
                ? "Доступных заказов пока нет"
                : "Доступных товаров пока нет"}
          </p>
        ) : (
          state.items.map((item) => (
            <button type="button" key={item.id} onClick={() => onSelect(item)}>
              {item.image && (
                <img
                  src={item.image}
                  alt=""
                  loading="lazy"
                  onError={(event) => {
                    event.currentTarget.hidden = true;
                  }}
                />
              )}
              <span>
                <strong>{item.label}</strong>
                {item.kind === "order" && <small>{chatOrderStatus(item.status)}</small>}
              </span>
            </button>
          ))
        )}
      </div>
    </div>
  );
}
