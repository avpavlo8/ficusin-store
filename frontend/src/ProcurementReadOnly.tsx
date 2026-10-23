import { useCallback, useEffect, useState } from "react";
import { receivingModeLabel } from "./procurementUX";
import { api, PageHeading } from "./adminShared";
import type { ProcurementData, ProcurementOrderDetail } from "./adminTypes";
import { useProcurementRoute, useProcurementParam } from "./useProcurementRoute";
import { procurementOrderHref, useProcurementDialog } from "./procurementUX";
import "./styles/admin-procurement-plan.css";
import "./styles/admin-procurement-redesign.css";
import "./styles/admin-procurement-review.css";
import "./styles/admin-procurement-workspace.css";

const statuses: Record<string, string> = {
  draft: "Черновик",
  ordered: "Заказано поставщику",
  invoice_received: "Инвойс загружен",
  review: "Сверка инвойса",
  ready_to_receive: "Ожидаем поступление",
  received: "Получено",
  cancelled: "Отменено",
};

export default function ProcurementReadOnly({ onError }: { onError: (message: string) => void }) {
  const [data, setData] = useState<ProcurementData | null>(null);
  const { selectedOrder, setSelectedOrder } = useProcurementRoute();
  const [query, setQuery] = useProcurementParam("q", "");
  const [status, setStatus] = useProcurementParam("status", "active");
  const load = useCallback(
    () =>
      api<ProcurementData>("/api/v1/admin/procurement")
        .then(setData)
        .catch((error) => onError((error as Error).message)),
    [onError],
  );
  useEffect(() => {
    void load();
  }, [load]);
  const orders =
    data?.orders.filter(
      (order) =>
        (status === "all" ||
          (status === "active"
            ? !["received", "cancelled", "draft"].includes(order.status)
            : order.status === status)) &&
        `${order.title} ${order.orderNumber} ${order.supplierName}`
          .toLocaleLowerCase("ru")
          .includes(query.toLocaleLowerCase("ru")),
    ) || [];
  return (
    <>
      <div className="procurement-page-heading">
        <PageHeading
          eyebrow="Поставки"
          title="Закупки"
          text="Состав заказов поставщикам и состояние поступления"
        />
        <button onClick={() => void load()}>Обновить</button>
      </div>
      <p className="procurement-note">
        Просмотр закупок. «Заказано» означает заказ поставщику; отправка и дата прибытия здесь не
        подтверждаются.
      </p>
      <section className="admin-block procurement-block">
        <div className="procurement-filter-bar">
          <input
            aria-label="Поиск закупки"
            placeholder="Название, номер или поставщик"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <select
            aria-label="Статус закупки"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="active">Ожидаем поступление</option>
            <option value="received">Полученные</option>
            <option value="cancelled">Отменённые</option>
            <option value="all">Все закупки</option>
          </select>
        </div>
        {!data ? (
          <p>Загружаем закупки…</p>
        ) : orders.length ? (
          <div className="admin-table-wrap">
            <table className="admin-table procurement-incoming-orders">
              <thead>
                <tr>
                  <th>Закупка</th>
                  <th>Поставщик</th>
                  <th>Состояние</th>
                  <th>Растений</th>
                </tr>
              </thead>
              <tbody>
                {orders.map((order) => (
                  <tr key={order.id}>
                    <td data-label="Закупка">
                      <a
                        href={procurementOrderHref(order.id)}
                        onClick={(event) => {
                          if (!event.metaKey && !event.ctrlKey) {
                            event.preventDefault();
                            setSelectedOrder(order.id);
                          }
                        }}
                      >
                        {order.title || order.orderNumber || `Закупка №${order.id}`}
                      </a>
                      <small>{new Date(order.createdAt).toLocaleDateString("ru-RU")}</small>
                    </td>
                    <td data-label="Поставщик">{order.supplierName}</td>
                    <td data-label="Состояние">{statuses[order.status] || order.status}</td>
                    <td data-label="Растений">
                      {order.units} шт.
                      {Boolean(order.pendingReview) && (
                        <small>Состав ещё сверяется с инвойсом</small>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p>Закупок по выбранному фильтру нет.</p>
        )}
      </section>
      {selectedOrder && (
        <ReadOnlyOrder
          key={selectedOrder}
          id={selectedOrder}
          onClose={() => setSelectedOrder(null)}
        />
      )}
    </>
  );
}

function ReadOnlyOrder({ id, onClose }: { id: number; onClose: () => void }) {
  const ref = useProcurementDialog(onClose);
  const [detail, setDetail] = useState<ProcurementOrderDetail | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  useEffect(() => {
    let current = true;
    void api<ProcurementOrderDetail>(`/api/v1/admin/procurement/orders/${id}`)
      .then((value) => {
        if (current) setDetail(value);
      })
      .catch((error) => {
        if (current) setError((error as Error).message);
      });
    return () => {
      current = false;
    };
  }, [id]);
  const lines = (detail?.lines || []).filter(
    (line) =>
      !line.invoiceExcluded &&
      (line.reconciliationStatus !== "added" || line.comparisonAccepted) &&
      `${line.sabyName} ${line.rawName} ${line.invoiceRawName} ${line.supplierArticle}`
        .toLocaleLowerCase("ru")
        .includes(query.toLocaleLowerCase("ru")),
  );
  return (
    <>
      <button
        tabIndex={-1}
        className="admin-dialog-backdrop"
        aria-label="Закрыть просмотр закупки"
        onClick={onClose}
      />
      <div
        ref={ref}
        className="admin-dialog procurement-order-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="manager-order-title"
      >
        <header>
          <h2 id="manager-order-title">
            {detail?.order.title || detail?.order.orderNumber || `Закупка №${id}`}
          </h2>
          <button aria-label="Закрыть закупку" onClick={onClose}>
            ×
          </button>
        </header>
        <div className="procurement-order-body">
          {error ? (
            <p role="alert">{error}</p>
          ) : !detail ? (
            <p>Загружаем состав…</p>
          ) : (
            <>
              <div className="procurement-order-summary">
                <strong>{detail.order.supplierName}</strong>
                <span>{statuses[detail.order.status] || detail.order.status}</span>
              </div>
              {Boolean(detail.order.pendingReview) && (
                <p className="procurement-note">Состав уточняется по инвойсу.</p>
              )}
              <input
                aria-label="Поиск растения в закупке"
                placeholder="Название или артикул растения"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
              />
              <div className="admin-table-wrap">
                <table className="admin-table procurement-incoming-lines">
                  <thead>
                    <tr>
                      <th>Растение</th>
                      <th>Размер</th>
                      <th>Заказано</th>
                      <th>По инвойсу</th>
                      <th>Сверка</th>
                    </tr>
                  </thead>
                  <tbody>
                    {lines.map((line) => (
                      <tr key={line.id}>
                        <td data-label="Растение">
                          <strong>
                            {line.receivingMode && line.receivingMode !== "catalog"
                              ? line.rawName
                              : line.sabyName || line.rawName}
                          </strong>
                          <small>{line.supplierArticle}</small>
                          <small>{line.invoiceRawName}</small>
                          {line.receivingMode && line.receivingMode !== "catalog" && (
                            <small>
                              {receivingModeLabel(line.receivingMode)}
                              {line.receivingAllocations?.map((part) => (
                                <span key={part.sabyId}>
                                  {part.name}: {part.quantity} шт.
                                </span>
                              ))}
                            </small>
                          )}
                        </td>
                        <td data-label="Размер">
                          {[
                            line.potDiameterCm && `D${line.potDiameterCm}`,
                            line.heightCm && `${line.heightCm} см`,
                          ]
                            .filter(Boolean)
                            .join(" · ") || "Не указан"}
                        </td>
                        <td data-label="Заказано">{line.orderedQuantity || "—"}</td>
                        <td data-label="По инвойсу">{line.invoicedQuantity ?? "—"}</td>
                        <td data-label="Сверка">
                          {line.reconciliationStatus === "missing"
                            ? "Нет в инвойсе"
                            : line.comparisonMismatch && !line.comparisonAccepted
                              ? "Уточняется"
                              : line.invoicedQuantity == null
                                ? "Ожидаем инвойс"
                                : "Сверено"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {!lines.length && <p>Растения не найдены.</p>}
            </>
          )}
        </div>
      </div>
    </>
  );
}
