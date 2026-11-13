import { useEffect, useRef, useState } from "react";
import { api, money } from "./adminShared";
import { WorkspaceState } from "./WorkspaceUI";
import "./styles/order-realizations.css";

type Realization = {
  key: string;
  orderId: number;
  orderNumber: string;
  shipmentOfferId: number;
  intentId: number;
  state: "not_prepared" | "queued" | "prepared";
  source: string;
  warehouseId: number;
  fingerprint: string;
  blockers: string[];
  canPrepare: boolean;
  lines: Array<{
    orderItemId: number;
    variantId: number;
    name: string;
    sku: string;
    sabyId: string;
    quantity: number;
    unitPrice: string;
  }>;
};
type View = {
  events: Realization[];
  warehouses: Array<{ id: number; name: string; nativeMappingConfirmed: boolean }>;
  hasMore: boolean;
  externalWritesEnabled: boolean;
};
export default function AdminOrderRealizations({ allowed }: { allowed: boolean }) {
  return allowed ? (
    <Realizations />
  ) : (
    <WorkspaceState kind="restricted" title="Реализации доступны владельцу" />
  );
}
function Realizations() {
  const [view, setView] = useState<View | null>(null);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [revision, setRevision] = useState(0);
  const [selected, setSelected] = useState<string[]>([]);
  const [warehouse, setWarehouse] = useState("");
  const [busy, setBusy] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const version = useRef(0);
  useEffect(() => {
    let active = true;
    const current = ++version.current;
    api<View>(`/api/v1/admin/order-realizations?${new URLSearchParams({ q: query })}`)
      .then((result) => {
        if (active && current === version.current) {
          setView(result);
          setError("");
          setSelected([]);
        }
      })
      .catch((caught) => {
        if (active)
          setError(caught instanceof Error ? caught.message : "Не удалось загрузить реализации");
      });
    return () => {
      active = false;
    };
  }, [query, revision]);
  const loadMore = async () => {
    if (!view || loadingMore) return;
    const current = version.current;
    setLoadingMore(true);
    try {
      const result = await api<View>(
        `/api/v1/admin/order-realizations?${new URLSearchParams({ q: query, offset: String(view.events.length) })}`,
      );
      if (current === version.current)
        setView((old) =>
          old
            ? {
                ...result,
                events: [
                  ...old.events,
                  ...result.events.filter(
                    (event) => !old.events.some((item) => item.key === event.key),
                  ),
                ],
              }
            : result,
        );
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось загрузить отправки");
    } finally {
      setLoadingMore(false);
    }
  };
  const prepare = async () => {
    if (!view || busy || !warehouse || !selected.length) return;
    const events = view.events.filter((event) => selected.includes(event.key));
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const result = await api<{ events: Realization[] }>(
        "/api/v1/admin/order-realizations/prepare",
        {
          method: "POST",
          body: JSON.stringify({
            warehouseId: Number(warehouse),
            events: events.map((event) => ({
              orderId: event.orderId,
              shipmentOfferId: event.shipmentOfferId,
              fingerprint: event.fingerprint,
            })),
          }),
        },
      );
      setView((old) =>
        old
          ? {
              ...old,
              events: old.events.map(
                (event) => result.events.find((item) => item.key === event.key) || event,
              ),
            }
          : old,
      );
      setSelected([]);
      setMessage("Подготовлено на сайте. Документы в СБИС не создавались, остатки не изменялись.");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось сохранить подготовку");
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="order-realizations" aria-labelledby="order-realizations-title">
      <header>
        <div>
          <a href="/admin?section=orders">← К заказам</a>
          <h1 id="order-realizations-title">Реализация по заказам</h1>
        </div>
        <button type="button" disabled={busy} onClick={() => setRevision((value) => value + 1)}>
          Обновить
        </button>
      </header>
      <p className="order-realizations-note">
        Доставка автоматически попадает сюда при передаче СДЭК, самовывоз — после выдачи. Старые
        заказы можно подготовить вручную.
      </p>
      {view && !view.externalWritesEnabled && (
        <p className="order-realizations-capability">
          Сейчас доступна подготовка на сайте. Создание реализации и изменение остатков в СБИС ещё
          не подключены. Связь склада сайта со складом СБИС требует настройки.
        </p>
      )}
      <form
        className="order-realizations-search"
        onSubmit={(event) => {
          event.preventDefault();
          setQuery(search.trim());
          setRevision((value) => value + 1);
        }}
      >
        <label>
          Номер заказа
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Найти старый заказ"
          />
        </label>
        <button type="submit" disabled={busy}>
          Найти
        </button>
      </form>
      {error && (
        <div className="admin-message error" role="alert">
          {error}
        </div>
      )}
      {message && <p role="status">{message}</p>}
      {!view && !error && <WorkspaceState kind="loading" title="Загружаем реализации…" />}
      {view && (
        <>
          <div className="order-realizations-actions">
            <label>
              Склад
              <select
                aria-label="Склад реализации"
                value={warehouse}
                disabled={busy}
                onChange={(event) => setWarehouse(event.target.value)}
              >
                <option value="">Выберите склад</option>
                {view.warehouses.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>
            <button
              type="button"
              disabled={busy || !warehouse || !selected.length}
              onClick={() => void prepare()}
            >
              {busy
                ? "Сохраняем…"
                : `Подготовить на сайте${selected.length ? ` (${selected.length})` : ""}`}
            </button>
          </div>
          {view.events.length === 0 ? (
            <WorkspaceState
              kind="empty"
              title="Переданных заказов нет"
              detail="Здесь появятся заказы, переданные СДЭК или выданные покупателю."
            />
          ) : (
            <ul className="order-realizations-list" aria-label="Отправки для реализации">
              {view.events.map((event) => (
                <li key={event.key} data-event={event.key}>
                  <div className="order-realizations-order">
                    <label>
                      <input
                        type="checkbox"
                        aria-label={`Выбрать заказ ${event.orderNumber}${event.shipmentOfferId ? `, отправка ${event.shipmentOfferId}` : ""}`}
                        checked={selected.includes(event.key)}
                        disabled={busy || !event.canPrepare || event.state === "prepared"}
                        onChange={(change) =>
                          setSelected((old) =>
                            change.target.checked
                              ? [...old, event.key]
                              : old.filter((key) => key !== event.key),
                          )
                        }
                      />
                      <a href={`/admin?section=orders&order=${event.orderId}`}>
                        {event.orderNumber}
                      </a>
                    </label>
                    {event.shipmentOfferId > 0 && <small>Отправка №{event.shipmentOfferId}</small>}
                    <small>
                      {event.source === "automatic" ? "Автоматически" : "Ручная подготовка"}
                    </small>
                  </div>
                  <ul className="order-realizations-lines">
                    {event.lines.map((line) => (
                      <li key={line.orderItemId}>
                        <span>
                          {line.name}
                          {line.sku && <small>{line.sku}</small>}
                        </span>
                        <span>
                          {line.quantity} шт. × {money.format(Number(line.unitPrice))}
                        </span>
                      </li>
                    ))}
                  </ul>
                  <div className="order-realizations-state">
                    <strong>
                      {event.state === "prepared"
                        ? "Подготовлено на сайте"
                        : event.state === "queued"
                          ? "В очереди подготовки"
                          : "Не подготовлено"}
                    </strong>
                    {event.intentId > 0 && <small>Подготовка №{event.intentId}</small>}
                    {event.warehouseId > 0 && (
                      <small>
                        {view.warehouses.find((item) => item.id === event.warehouseId)?.name ||
                          "Выбранный склад"}
                      </small>
                    )}
                    {event.blockers.map((reason) => (
                      <p key={reason}>{reason}</p>
                    ))}
                  </div>
                </li>
              ))}
            </ul>
          )}
          {view.hasMore && (
            <button type="button" disabled={loadingMore || busy} onClick={() => void loadMore()}>
              {loadingMore ? "Загружаем…" : "Показать ещё"}
            </button>
          )}
        </>
      )}
    </section>
  );
}
