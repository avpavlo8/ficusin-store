import { useEffect, useRef, useState } from "react";
import { api, money } from "./adminShared";
import { WorkspaceState } from "./WorkspaceUI";
import "./styles/order-realizations.css";

type Publication = {
  id: number;
  executionMode: "mock" | "live";
  testMode?: boolean;
  status: "queued" | "processing" | "posted" | "needs_attention" | "failed";
  message: string;
  documentId: string;
  documentUUID: string;
  stockVerified: boolean;
  canRetry: boolean;
};
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
  publication?: Publication;
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
  executionMode?: "disabled" | "mock" | "live";
  dispatchMode?: "disabled" | "mock" | "live";
  dispatchEnabled?: boolean;
  autoDispatchEnabled?: boolean;
};

function dispatchMode(view: View | null) {
  const mode = view?.executionMode || view?.dispatchMode || "disabled";
  const enabled =
    view?.dispatchEnabled ?? Boolean(view?.autoDispatchEnabled || view?.externalWritesEnabled);
  return enabled && (mode === "mock" || (mode === "live" && view?.externalWritesEnabled))
    ? mode
    : "disabled";
}
function selectable(event: Realization, mode: ReturnType<typeof dispatchMode>) {
  return (
    event.canPrepare && !event.publication && (event.state !== "prepared" || mode !== "disabled")
  );
}
function PublicationState({
  publication,
  busy,
  onRetry,
}: {
  publication: Publication;
  busy: boolean;
  onRetry: () => void;
}) {
  const test =
    publication.executionMode === "mock" ||
    publication.testMode ||
    publication.documentId.startsWith("STG-");
  const verified =
    !test &&
    publication.executionMode === "live" &&
    publication.status === "posted" &&
    publication.stockVerified &&
    Boolean(publication.documentId.trim() && publication.documentUUID.trim());
  const title =
    publication.status === "posted"
      ? test
        ? "Тестовая реализация завершена"
        : verified
          ? "Проведено в СБИС"
          : "Проведение не подтверждено"
      : {
          queued: "В очереди обмена",
          processing: "Обрабатывается",
          needs_attention: "Нужна сверка",
          failed: "Ошибка обмена",
        }[publication.status];
  const state =
    publication.status === "posted" && !test && !verified ? "needs_attention" : publication.status;
  return (
    <div
      className="order-realizations-publication"
      data-test={test ? "true" : undefined}
      aria-live="polite"
    >
      <strong className="order-realizations-publication-label" data-state={state}>
        {test && publication.status !== "posted" ? `Тест · ${title}` : title}
      </strong>
      {publication.documentId && (
        <small>
          {test ? "Тестовый документ" : "Документ СБИС"}: {publication.documentId}
        </small>
      )}
      {test ? (
        <p className="order-realizations-test-note">
          Документ СБИС и реальные списания не создавались.
        </p>
      ) : publication.status === "posted" && !verified ? (
        <p>Нет подтверждения документа и складского движения. Требуется проверка.</p>
      ) : verified ? (
        <p className="order-realizations-proof">Складское движение подтверждено.</p>
      ) : (
        publication.message && <p>{publication.message}</p>
      )}
      {test &&
        (publication.status === "failed" || publication.status === "needs_attention") &&
        publication.message && <p>{publication.message}</p>}
      {publication.canRetry && (
        <button type="button" disabled={busy} onClick={onRetry}>
          Повторить проверку
        </button>
      )}
    </div>
  );
}
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
  const [refreshError, setRefreshError] = useState("");
  const version = useRef(0);
  const mode = dispatchMode(view);
  const pending = Boolean(
    view?.events.some(
      (event) => event.publication && ["queued", "processing"].includes(event.publication.status),
    ),
  );
  const loadedCount = view?.events.length || 0;
  useEffect(() => {
    let active = true;
    const current = ++version.current;
    api<View>(`/api/v1/admin/order-realizations?${new URLSearchParams({ q: query })}`)
      .then((result) => {
        if (active && current === version.current) {
          setView(result);
          setError("");
          setRefreshError("");
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
  useEffect(() => {
    if (!pending || busy) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    const current = version.current;
    const poll = async () => {
      try {
        if (!document.hidden) {
          let result: View | undefined;
          const events: Realization[] = [];
          for (let offset = 0; offset < Math.max(50, loadedCount); offset += 50) {
            if (!active || current !== version.current) return;
            result = await api<View>(
              `/api/v1/admin/order-realizations?${new URLSearchParams({ q: query, offset: String(offset) })}`,
            );
            events.push(...result.events);
            if (!result.hasMore) break;
          }
          if (active && current === version.current && result) {
            setView({ ...result, events });
            if (
              !events.some(
                (event) =>
                  event.publication && ["queued", "processing"].includes(event.publication.status),
              )
            )
              setMessage("");
            const available = new Set(
              events
                .filter((event) => selectable(event, dispatchMode(result!)))
                .map((event) => event.key),
            );
            setSelected((old) => old.filter((key) => available.has(key)));
            setRefreshError("");
          }
        }
      } catch {
        if (active)
          setRefreshError(
            "Не удалось обновить состояние обмена. Попробуем ещё раз или нажмите «Обновить».",
          );
      } finally {
        if (active) timer = setTimeout(poll, 5000);
      }
    };
    timer = setTimeout(poll, 3000);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [pending, busy, query, loadedCount, revision]);
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
      const publications = result.events.map((event) => event.publication);
      setMessage(
        publications.length && publications.every((item) => item?.executionMode === "mock")
          ? "Тестовые реализации поставлены в очередь. Реальные документы и остатки СБИС не меняются."
          : publications.length && publications.every((item) => item?.executionMode === "live")
            ? "Реализации поставлены в очередь СБИС. Результат появится после проверки проведения."
            : "Подготовлено на сайте. Документы в СБИС не создавались, остатки не изменялись.",
      );
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось сохранить подготовку");
    } finally {
      setBusy(false);
    }
  };
  const retry = async (publication: Publication) => {
    if (busy || !publication.canRetry) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await api(`/api/v1/admin/order-realizations/${publication.id}/retry`, {
        method: "POST",
        body: "{}",
      });
      setMessage("Повторная проверка поставлена в очередь.");
      setRevision((value) => value + 1);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось повторить проверку");
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
      {view && (
        <p className="order-realizations-capability" data-mode={mode}>
          {mode === "mock"
            ? "Тестовый обмен. Проверяем создание реализаций без документов и реальных списаний в СБИС."
            : mode === "live"
              ? `Обмен с СБИС включён. ${view.autoDispatchEnabled ? "Новые подтверждённые отправки обрабатываются автоматически." : "Выбранные заказы можно отправить вручную."} Проведение подтверждаем по документу и складскому движению.`
              : "Сейчас доступна подготовка на сайте. Создание реализации и изменение остатков в СБИС ещё не подключены. Связь склада сайта со складом СБИС требует настройки."}
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
      {refreshError && <p role="status">{refreshError}</p>}
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
                  <option
                    key={item.id}
                    value={item.id}
                    disabled={mode === "live" && !item.nativeMappingConfirmed}
                  >
                    {item.name}
                    {mode === "live" && !item.nativeMappingConfirmed ? " — не связан с СБИС" : ""}
                  </option>
                ))}
              </select>
            </label>
            <button
              type="button"
              disabled={
                busy ||
                !warehouse ||
                !selected.length ||
                (mode === "live" &&
                  !view.warehouses.some(
                    (item) => String(item.id) === warehouse && item.nativeMappingConfirmed,
                  ))
              }
              onClick={() => void prepare()}
            >
              {busy
                ? "Сохраняем…"
                : `${mode === "mock" ? "Создать тестовую реализацию" : mode === "live" ? "Создать реализацию" : "Подготовить на сайте"}${selected.length ? ` (${selected.length})` : ""}`}
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
                        disabled={busy || !selectable(event, mode)}
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
                    {event.publication ? (
                      <PublicationState
                        publication={event.publication}
                        busy={busy}
                        onRetry={() => void retry(event.publication!)}
                      />
                    ) : (
                      <strong>
                        {event.state === "prepared"
                          ? "Подготовлено на сайте"
                          : event.state === "queued"
                            ? "В очереди подготовки"
                            : "Не подготовлено"}
                      </strong>
                    )}
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
