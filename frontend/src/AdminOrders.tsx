import { lazy, Suspense, useCallback, useEffect, useRef, useState, type MouseEvent } from "react";
import { api, money, paymentLabels, paymentMethodLabels, statusLabels } from "./adminShared";
import { currentAdminSection } from "./adminNavigation";
import { WorkspaceState, WorkspaceStatus, WorkspaceTable } from "./WorkspaceUI";
import type { Order } from "./adminTypes";
import "./styles/admin-orders-navigation.css";

const AdminOrderWorkspace = lazy(() =>
  import("./AdminOrderWorkspace").then((module) => ({ default: module.AdminOrderWorkspace })),
);
const filters = [
  { id: "new", label: "Новые", empty: "Новых заказов пока нет" },
  { id: "in_progress", label: "В работе", empty: "Заказов в работе пока нет" },
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
  const unpaidConfirmed = order.status === "confirmed" && order.paymentStatus !== "paid";
  if (filter === "new") return order.status === "new" || unpaidConfirmed;
  if (filter === "in_progress")
    return (
      ["confirmed", "assembling", "ready", "shipped"].includes(order.status) && !unpaidConfirmed
    );
  return order.status === filter;
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
      if (!next.orderId) heading.current?.focus();
      window.scrollTo({ top: 0 });
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
    api<{ orders: Order[] }>("/api/v1/admin/orders")
      .then((data) => {
        if (!cancelled) setItems(data.orders);
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setLoadError(error.message);
          onError(error.message);
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
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
  const back = (
    <a
      className="orders-back-link"
      href={ordersUrl(listNavigation)}
      onClick={(event) => follow(event, listNavigation)}
    >
      <svg
        viewBox="0 0 24 24"
        width="18"
        height="18"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        aria-hidden="true"
      >
        <path d="m14 6-6 6 6 6M8 12h12" />
      </svg>
      К заказам
    </a>
  );

  if (navigation.orderId)
    return (
      <>
        {back}
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
            detail="Проверьте ссылку или вернитесь к списку заказов."
          />
        )}
      </>
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
        <p>Откройте номер заказа, чтобы перейти к управлению.</p>
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
        <WorkspaceTable label="Заказы">
          <thead>
            <tr>
              <th>Заказ</th>
              <th>Клиент</th>
              <th>Получение</th>
              <th>Сумма</th>
              <th>Статус</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((order) => {
              const next = { ...navigation, orderId: String(order.id) };
              return (
                <tr key={order.id}>
                  <td>
                    <a
                      className="orders-number-link"
                      href={ordersUrl(next)}
                      onClick={(event) => follow(event, next)}
                    >
                      {order.orderNumber}
                    </a>
                    <small>{new Date(order.createdAt).toLocaleString("ru-RU")}</small>
                  </td>
                  <td>
                    <strong>{order.customerName}</strong>
                    <a href={`tel:${order.phone}`}>{order.phone}</a>
                    <small>{order.email}</small>
                  </td>
                  <td>
                    <strong>{deliveryLabel(order.deliveryMethod)}</strong>
                    <small>{order.address}</small>
                    {order.trackNumber && <small>Трек: {order.trackNumber}</small>}
                    {order.status !== "cancelled" && order.hasPreorder && (
                      <small className="admin-flag">Нужно проверить наличие</small>
                    )}
                    {order.status !== "cancelled" && order.deliveryFeePending && (
                      <small className="admin-flag">
                        {order.repackRequested
                          ? "Запрошена одна коробка"
                          : "Доставка требует уточнения"}
                      </small>
                    )}
                  </td>
                  <td>
                    <strong>{money.format(order.total)}</strong>
                    <small
                      className={
                        order.paymentStatus === "paid"
                          ? "payment-state paid"
                          : order.status !== "cancelled" &&
                              ["pending", "partially_paid"].includes(order.paymentStatus)
                            ? "payment-state unpaid"
                            : "payment-state"
                      }
                    >
                      {paymentLabel(order)}
                    </small>
                    {order.status !== "cancelled" &&
                      order.paymentMethod &&
                      order.paymentMethod !== "online" && (
                        <small className="admin-flag">
                          {paymentMethodLabels[order.paymentMethod]}
                        </small>
                      )}
                  </td>
                  <td>
                    <WorkspaceStatus status={order.status}>
                      {statusLabels[order.status] || order.status}
                    </WorkspaceStatus>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </WorkspaceTable>
      )}
    </section>
  );
}
