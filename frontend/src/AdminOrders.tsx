import { OrderProductImage } from "./OrderProductImage";
import { OrderProductLink } from "./OrderProductLink";
import { lazy, Suspense, useCallback, useEffect, useRef, useState, type MouseEvent } from "react";
import { api, money, paymentLabels, statusLabels } from "./adminShared";
import { currentAdminSection } from "./adminNavigation";
import { WorkspaceState } from "./WorkspaceUI";
import type { Order } from "./adminTypes";
import "./styles/admin-orders-navigation.css";

const AdminOrderWorkspace = lazy(() =>
  import("./AdminOrderWorkspace").then((module) => ({ default: module.AdminOrderWorkspace })),
);
const filters = [
  { id: "new", label: "Новые", empty: "Новых заказов пока нет" },
  { id: "in_progress", label: "В работе", empty: "Заказов в работе пока нет" },
  { id: "in_delivery", label: "В доставке", empty: "Заказов в доставке пока нет" },
  { id: "completed", label: "Завершённые", empty: "Завершённых заказов пока нет" },
  { id: "all", label: "Все", empty: "Заказов пока нет" },
  { id: "cancelled", label: "Отменённые", empty: "Отменённых заказов пока нет" },
] as const;
type Filter = (typeof filters)[number]["id"];
type Navigation = { filter: Filter; query: string; orderId: string };
type LeaveGuard = () => Promise<boolean>;
type RegisterGuard = (guard: LeaveGuard) => () => void;

function readNavigation(): Navigation {
  const params = new URLSearchParams(window.location.search);
  const status = params.get("status");
  return {
    filter: filters.find((filter) => filter.id === status)?.id ?? "new",
    query: params.get("q") ?? "",
    orderId: params.get("order") ?? params.get("customerOrderId") ?? "",
  };
}

function ordersUrl(nav: Navigation): string {
  const params = new URLSearchParams({ section: "orders", status: nav.filter });
  if (nav.query) params.set("q", nav.query);
  if (nav.orderId) params.set("order", nav.orderId);
  return `/admin?${params}`;
}

function matchesFilter(order: Order, filter: Filter) {
  if (filter === "all") return true;
  const queue =
    order.workflow?.queue ||
    (["shipped", "shipping"].includes(order.status)
      ? "in_delivery"
      : ["assembling", "ready"].includes(order.status)
        ? "in_progress"
        : ["new", "confirmed"].includes(order.status)
          ? "new"
          : order.status);
  return queue === filter;
}

function paymentLabel(order: Order) {
  if (
    order.status === "cancelled" &&
    ["pending", "manager_confirmation", "payment_provider_pending", "on_delivery"].includes(
      order.paymentStatus,
    )
  )
    return "Оплата не требуется";
  return paymentLabels[order.paymentStatus] ?? order.paymentStatus;
}

const deliveryLabel = (value: string) =>
  value === "pickup" ? "Самовывоз" : value === "cdek" ? "СДЭК" : value;

type OrderWarning = NonNullable<Order["workflow"]>["blockers"][number];

function orderWarnings(order: Order): OrderWarning[] {
  if (order.workflow) {
    if (order.workflow.queue !== "new") return [];
    // Payment already has its own column. Keep the remaining blockers available
    // without repeating the same state for every line of the order.
    return order.workflow.blockers.filter(
      (blocker) => !["payment", "pickup_payment"].includes(blocker.code),
    );
  }
  if (["cancelled", "completed", "shipped", "shipping"].includes(order.status)) return [];
  return [
    ...(order.hasPreorder ? [{ code: "stock_unknown", message: "Нужно проверить наличие" }] : []),
    ...(order.deliveryFeePending
      ? [
          {
            code: "delivery_fee",
            message: order.repackRequested
              ? "Запрошена одна коробка"
              : "Доставка требует уточнения",
          },
        ]
      : []),
  ];
}

function shortWarning(warning: OrderWarning) {
  if (warning.code === "dimensions") return "Нет габаритов";
  if (warning.code === "stock_short") return "Не хватает в наличии";
  if (warning.code === "stock_unknown") return "Проверить остаток";
  return warning.message;
}

export function Orders({
  canRefund = false,
  focusOrder,
  onError,
  registerBeforeLeave,
}: {
  canRefund?: boolean;
  focusOrder?: string;
  onError: (value: string) => void;
  registerBeforeLeave?: RegisterGuard;
}) {
  const [items, setItems] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [navigation, setNavigation] = useState(readNavigation);
  const navigationRef = useRef(navigation);
  const guard = useRef<LeaveGuard | null>(null);
  const handledFocus = useRef("");
  const heading = useRef<HTMLHeadingElement>(null);

  const registerGuard = useCallback<RegisterGuard>(
    (callback) => {
      guard.current = callback;
      const unregister = registerBeforeLeave?.(callback);
      return () => {
        if (guard.current === callback) guard.current = null;
        unregister?.();
      };
    },
    [registerBeforeLeave],
  );

  const applyNavigation = useCallback(
    (next: Navigation, mode: "push" | "replace" | "read" = "push") => {
      if (mode !== "read")
        window.history[mode === "replace" ? "replaceState" : "pushState"](
          null,
          "",
          ordersUrl(next),
        );
      navigationRef.current = next;
      setNavigation(next);
    },
    [],
  );

  const navigate = async (next: Navigation) => {
    if (guard.current && !(await guard.current())) return;
    applyNavigation(next);
    window.requestAnimationFrame(() => {
      document
        .querySelector<HTMLElement>(".admin-order-workspace h1, .admin-order-workspace h2")
        ?.focus();
      if (!next.orderId && !navigationRef.current.orderId) heading.current?.focus();
    });
  };

  const follow = (event: MouseEvent<HTMLAnchorElement>, next: Navigation) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    void navigate(next);
  };

  useEffect(() => {
    let cancelled = false;
    let loaded = false;
    let pending = false;
    const controller = new AbortController();
    const read = async () => {
      if (pending) return;
      pending = true;
      try {
        const data = await api<{ orders: Order[] }>("/api/v1/admin/orders", {
          signal: controller.signal,
        });
        if (!cancelled) {
          setItems(data.orders);
          setLoadError("");
          loaded = true;
        }
      } catch (error) {
        if (!cancelled && !loaded) {
          const message = (error as Error).message;
          setLoadError(message);
          onError(message);
        }
      } finally {
        pending = false;
        if (!cancelled) setLoading(false);
      }
    };
    const refresh = () => {
      // Do not replace a composition being edited. The overview follows worker
      // and carrier updates without requiring a manual page reload.
      if (document.visibilityState === "visible" && !navigationRef.current.orderId) void read();
    };
    void read();
    const timer = window.setInterval(refresh, 15000);
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      cancelled = true;
      controller.abort();
      clearInterval(timer);
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [onError]);

  useEffect(() => {
    if (loading || !focusOrder || handledFocus.current === focusOrder) return;
    handledFocus.current = focusOrder;
    const match = items.find((order) =>
      focusOrder.startsWith("id:")
        ? order.id === Number(focusOrder.slice(3))
        : order.orderNumber === focusOrder,
    );
    if (!match) return;
    const timer = window.setTimeout(
      () => applyNavigation({ ...navigationRef.current, orderId: String(match.id) }, "replace"),
      0,
    );
    return () => window.clearTimeout(timer);
  }, [focusOrder, items, loading, applyNavigation]);

  useEffect(() => {
    const read = async () => {
      if (currentAdminSection() !== "orders") return;
      const next = readNavigation();
      if (
        next.orderId !== navigationRef.current.orderId &&
        guard.current &&
        !(await guard.current())
      ) {
        window.history.pushState(null, "", ordersUrl(navigationRef.current));
        return;
      }
      applyNavigation(next, "read");
    };
    const reset = () => {
      if (currentAdminSection() === "orders") applyNavigation(readNavigation(), "read");
    };
    window.addEventListener("popstate", read);
    window.addEventListener("admin:navigate", reset);
    return () => {
      window.removeEventListener("popstate", read);
      window.removeEventListener("admin:navigate", reset);
    };
  }, [applyNavigation]);

  const updateOrder = (order: Order) =>
    setItems((current) => current.map((item) => (item.id === order.id ? order : item)));
  const updateStatus = async (order: Order, status: string) => {
    const confirmPayment =
      status === "completed" &&
      order.deliveryMethod === "pickup" &&
      order.paymentMethod === "on_delivery" &&
      order.paymentStatus !== "paid";
    if (
      confirmPayment &&
      !window.confirm(`Подтвердите, что покупатель оплатил заказ ${order.orderNumber} при выдаче.`)
    )
      return;
    try {
      const result = await api<{ order: Order }>(`/api/v1/admin/orders/${order.id}`, {
        method: "PATCH",
        body: JSON.stringify({ status, paymentStatus: confirmPayment ? "paid" : "" }),
      });
      updateOrder(result.order);
    } catch (error) {
      onError((error as Error).message);
    }
  };

  const selected = items.find((order) => String(order.id) === navigation.orderId);
  const listNavigation = { ...navigation, orderId: "" };

  // Заказ открывается отдельной страницей, как карточка в кабинете продавца
  // Ozon: на ней достаточно места для пути заказа, «что сделать сейчас» и
  // итога справа. «К заказам» возвращает в ту же очередь с тем же поиском.
  if (navigation.orderId)
    return (
      <section className="order-page">
        <a
          className="order-page-back"
          href={ordersUrl(listNavigation)}
          onClick={(event) => follow(event, listNavigation)}
        >
          <span aria-hidden="true">← </span>К заказам
        </a>
        {loading ? (
          <WorkspaceState kind="loading" title="Загружаем заказ…" />
        ) : loadError ? (
          <WorkspaceState kind="error" title={loadError} />
        ) : selected ? (
          <Suspense fallback={<WorkspaceState kind="loading" title="Загружаем заказ…" />}>
            <AdminOrderWorkspace
              canRefund={canRefund}
              key={selected.id}
              order={selected}
              onSaved={updateOrder}
              onError={onError}
              onStatusChange={updateStatus}
              registerBeforeLeave={registerGuard}
            />
          </Suspense>
        ) : (
          <WorkspaceState
            kind="empty"
            title="Заказ не найден"
            detail="Возможно, ссылка устарела. Вернитесь к списку и найдите заказ поиском."
          />
        )}
      </section>
    );

  const query = navigation.query.trim().toLocaleLowerCase("ru-RU");
  const visible = items.filter(
    (order) =>
      matchesFilter(order, navigation.filter) &&
      `${order.orderNumber} ${order.customerName} ${order.phone} ${order.email}`
        .toLocaleLowerCase("ru-RU")
        .includes(query),
  );

  return (
    <section className="orders-list-page">
      <header className="orders-list-heading">
        <h1 ref={heading} tabIndex={-1}>
          Заказы
        </h1>
      </header>
      <nav className="orders-status-tabs" aria-label="Статусы заказов">
        {filters.map((filter) => {
          const next = { ...navigation, filter: filter.id };
          return (
            <a
              key={filter.id}
              href={ordersUrl(next)}
              aria-current={navigation.filter === filter.id ? "page" : undefined}
              onClick={(event) => follow(event, next)}
            >
              {filter.label}
              {!loading && !loadError && (
                <span>{items.filter((order) => matchesFilter(order, filter.id)).length}</span>
              )}
            </a>
          );
        })}
      </nav>
      <div className="admin-toolbar orders-search">
        <input
          type="search"
          aria-label="Найти заказ"
          placeholder="Номер, имя, телефон или email"
          value={navigation.query}
          onChange={(event) =>
            applyNavigation({ ...navigation, query: event.target.value }, "replace")
          }
        />
        {!loading && !loadError && <span aria-live="polite">Найдено: {visible.length}</span>}
      </div>
      {items.length >= 1000 && (
        <p className="admin-hint">
          Показаны последние 1000 заказов. Фильтры и поиск работают в этом списке.
        </p>
      )}
      {loading ? (
        <WorkspaceState kind="loading" title="Загружаем заказы…" />
      ) : loadError ? (
        <WorkspaceState kind="error" title={loadError} />
      ) : !visible.length ? (
        <WorkspaceState
          kind="empty"
          title={
            query
              ? "Заказы не найдены"
              : filters.find((filter) => filter.id === navigation.filter)!.empty
          }
          detail={
            query
              ? "Измените поиск или выберите другую вкладку."
              : navigation.filter === "new"
                ? "Принятые заказы появятся здесь."
                : undefined
          }
        />
      ) : (
        <div className="orders-table-wrap">
          <table className="orders-table" aria-label="Заказы" role="table">
            <thead role="rowgroup">
              <tr role="row">
                <th scope="col" className="orders-column-order">
                  Заказ
                </th>
                <th scope="col" className="orders-column-products">
                  Товары
                </th>
                <th scope="col" className="orders-column-customer">
                  Покупатель
                </th>
                <th scope="col" className="orders-column-delivery">
                  Доставка
                </th>
                <th scope="col" className="orders-column-status">
                  Статус
                </th>
                <th scope="col" className="orders-column-payment">
                  Оплата
                </th>
                <th scope="col" className="orders-column-total">
                  Сумма
                </th>
              </tr>
            </thead>
            <tbody role="rowgroup">
              {visible.map((order) => {
                const next = { ...navigation, orderId: String(order.id) };
                const warnings = orderWarnings(order);
                const itemIds = new Set(order.items.flatMap((item) => (item.id ? [item.id] : [])));
                const generalWarnings = warnings.filter(
                  (warning) => !warning.orderItemIds?.some((id) => itemIds.has(id)),
                );
                const onlyStockWarnings = generalWarnings.every((warning) =>
                  warning.code.startsWith("stock_"),
                );
                const manualAssembly =
                  order.workflow?.queue === "new" &&
                  order.deliveryMethod === "pickup" &&
                  order.paymentMethod === "on_delivery" &&
                  order.paymentStatus !== "paid";
                return (
                  <tr className="orders-card" key={order.id} role="row">
                    <td className="orders-cell-order" role="cell">
                      <a
                        className="orders-number-link"
                        href={ordersUrl(next)}
                        onClick={(event) => follow(event, next)}
                      >
                        {order.orderNumber}
                      </a>
                      <time dateTime={order.createdAt}>
                        {new Date(order.createdAt).toLocaleString("ru-RU", {
                          day: "2-digit",
                          month: "2-digit",
                          year: "numeric",
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </time>
                      {manualAssembly && (
                        <small className="orders-manual-hint">Сборка вручную</small>
                      )}
                      {generalWarnings.length > 0 && (
                        <details className="orders-attention">
                          <summary>
                            <svg
                              viewBox="0 0 16 16"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="1.5"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              aria-hidden="true"
                            >
                              <path d="m6 4 4 4-4 4" />
                            </svg>
                            <span>
                              {onlyStockWarnings ? "Проверить остатки" : "Нужна проверка"}
                            </span>
                            {generalWarnings.length > 1 && (
                              <span className="orders-attention-count">
                                {generalWarnings.length}
                              </span>
                            )}
                          </summary>
                          <ul
                            className="orders-card-blockers"
                            aria-label="Почему заказ ещё не на сборке"
                            role="list"
                          >
                            {generalWarnings.map((warning, index) => (
                              <li key={`${warning.code}:${index}`}>{warning.message}</li>
                            ))}
                          </ul>
                        </details>
                      )}
                    </td>
                    <td className="orders-cell-products" role="cell">
                      <div className="orders-card-products">
                        {order.items.map((item, index) => (
                          <div className="orders-card-product" key={index}>
                            <OrderProductImage item={item} />
                            <div>
                              <OrderProductLink item={item} />
                              <span>{item.variantLabel || item.sku}</span>
                              {warnings.some(
                                (warning) =>
                                  item.id !== undefined && warning.orderItemIds?.includes(item.id),
                              ) && (
                                <ul
                                  className="orders-item-issues"
                                  role="list"
                                  aria-label={`Причины проверки: ${item.productName}, ${item.variantLabel || item.sku}`}
                                >
                                  {warnings
                                    .filter(
                                      (warning) =>
                                        item.id !== undefined &&
                                        warning.orderItemIds?.includes(item.id),
                                    )
                                    .map((warning, index) => (
                                      <li
                                        className="orders-item-issue"
                                        data-code={warning.code}
                                        key={`${warning.code}:${index}`}
                                        title={warning.message}
                                      >
                                        {shortWarning(warning)}
                                      </li>
                                    ))}
                                </ul>
                              )}
                            </div>
                            <span className="orders-card-quantity">{item.quantity} шт.</span>
                          </div>
                        ))}
                      </div>
                    </td>
                    <td className="orders-cell-customer" data-label="Покупатель" role="cell">
                      <span>{order.customerName}</span>
                      <small>{order.phone}</small>
                    </td>
                    <td className="orders-cell-delivery" data-label="Доставка" role="cell">
                      <span>{deliveryLabel(order.deliveryMethod)}</span>
                      {order.trackNumber && <small>Трек: {order.trackNumber}</small>}
                    </td>
                    <td className="orders-cell-status" role="cell">
                      <span className="orders-status-label" data-status={order.status}>
                        {statusLabels[order.status] || order.status}
                      </span>
                    </td>
                    <td className="orders-cell-payment" data-label="Оплата" role="cell">
                      <span
                        className="payment-state"
                        data-tone={
                          order.paymentStatus === "paid"
                            ? "paid"
                            : order.paymentStatus === "on_delivery" && order.status !== "cancelled"
                              ? "on-delivery"
                              : "neutral"
                        }
                      >
                        {paymentLabel(order)}
                      </span>
                    </td>
                    <td className="orders-cell-total" role="cell">
                      <strong>{money.format(order.total)}</strong>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
