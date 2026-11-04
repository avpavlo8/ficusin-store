import { useMemo, useState } from "react";
import { AdminOrderEditor, type OrderReviewState, type OrderView } from "./AdminOrderEditor";
import { orderStatuses, paymentLabels, paymentMethodLabels, statusLabels } from "./adminShared";
import type { Order } from "./adminTypes";
import "./styles/admin-order-workspace.css";

type Props = {
  canRefund?: boolean;
  order: Order;
  onClose: () => void;
  onSaved: (order: Order) => void;
  onError: (message: string) => void;
  onStatusChange: (order: Order, status: string) => Promise<void>;
  registerBeforeLeave?: (guard: () => Promise<boolean>) => () => void;
};

const dateFormat = new Intl.DateTimeFormat("ru-RU", {
  day: "numeric",
  month: "long",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
});
const deliveryLabels: Record<string, string> = {
  pickup: "Самовывоз",
  cdek: "СДЭК",
  courier: "Курьер",
};

export function AdminOrderWorkspace({
  canRefund = false,
  order,
  onClose,
  onSaved,
  onError,
  onStatusChange,
  registerBeforeLeave,
}: Props) {
  const [review, setReview] = useState<OrderReviewState | null>(null);
  const [dirty, setDirty] = useState(false);
  const [changingStatus, setChangingStatus] = useState(false);
  const [view, setView] = useState<OrderView>("summary");
  const closed = ["cancelled", "completed"].includes(order.status);
  const reasons = useMemo(() => {
    const result: Array<{ title: string; text: string; view?: OrderView }> = [];
    if (!closed) {
      if (review?.hasPreorder ?? order.hasPreorder)
        result.push({
          title: "Подтвердить наличие растений",
          view: "items",
          text: "Наличие части растений требует подтверждения. Проверьте актуальный остаток и согласуйте состав заказа.",
        });
      if (review?.deliveryFeePending ?? order.deliveryFeePending)
        result.push(
          order.repackRequested
            ? {
                title: "Собрать в одну коробку и рассчитать доставку",
                view: "delivery",
                text: "Покупатель просит объединить растения в одну коробку. Проверьте упаковку и стоимость доставки.",
              }
            : order.deliveryMethod === "cdek" && review?.packagingDimensionsMissing
              ? {
                  title: "Заполните размеры коробки",
                  view: "delivery",
                  text: "Для расчёта доставки не хватает размеров упаковки. Укажите фактические размеры и вес коробки ниже.",
                }
              : {
                  title: "Рассчитать доставку",
                  view: "delivery",
                  text: "Стоимость доставки не рассчитана. Проверьте упаковку и уточните стоимость.",
                },
        );
      if (order.paymentMethod === "manager_confirmation")
        result.push({
          title: "Подтвердить заказ перед оплатой",
          text: "Выбран способ оплаты после подтверждения менеджером.",
        });
      if (order.paymentMethod === "invoice")
        result.push({
          title: "Подготовить счёт",
          text: "Покупатель выбрал оплату по счёту на организацию.",
        });
      if (order.deliveryMethod === "cdek" && review?.cdekCreateState === "manual_review")
        result.push({
          title: "Проверить отправление СДЭК",
          text:
            review.cdekLastError ||
            "Нужно сверить отправление в кабинете СДЭК. Новая заявка автоматически не создаётся.",
        });
      if (order.deliveryMethod === "cdek" && review?.cdekCreateState === "unknown")
        result.push({
          title: "Уточняется создание отправления",
          text: "СДЭК не подтвердил результат. Система ищет заявку по номеру заказа без повторного создания.",
        });
      if (
        order.deliveryMethod === "cdek" &&
        review?.cdekCreateState === "retry" &&
        review.cdekLastError !== "не хватает пункта выдачи или тарифа"
      )
        result.push({
          title: "СДЭК ожидает повторной попытки",
          text:
            review.cdekLastError ||
            "Предыдущая попытка создания отправления не завершилась успешно.",
        });
    }
    if (review?.paymentIssues.length)
      result.push({
        title: "Уточнить результат оплаты",
        view: "payment",
        text: review.paymentIssues.some((issue) => issue.needsReview)
          ? "Автопроверка остановлена. Сверьте платёж в кабинете ЮKassa."
          : "ЮKassa ещё не подтвердила результат. Проверка продолжается автоматически.",
      });
    return result;
  }, [order, review, closed]);
  const paymentLabel =
    order.status === "cancelled" &&
    ["pending", "manager_confirmation", "payment_provider_pending", "on_delivery"].includes(
      order.paymentStatus,
    )
      ? "Оплата не требуется"
      : order.paymentStatus === "partially_paid"
        ? "Частично оплачен"
        : paymentLabels[order.paymentStatus] || order.paymentStatus;

  const changeStatus = async (status: string) => {
    setChangingStatus(true);
    try {
      await onStatusChange(order, status);
    } finally {
      setChangingStatus(false);
    }
  };

  const customerCard = (
    <section className="ws-card" aria-labelledby="order-customer-title">
      <div className="ws-card-head">
        <h3 id="order-customer-title">Покупатель и получение</h3>
      </div>
      <dl className="ws-facts">
        <dt>Покупатель</dt>
        <dd>
          <strong>{order.customerName || "Имя не указано"}</strong>
          {order.phone && (
            <>
              <br />
              <a href={`tel:${order.phone}`}>{order.phone}</a>
            </>
          )}
          {order.email && (
            <>
              <br />
              <a href={`mailto:${order.email}`}>{order.email}</a>
            </>
          )}
        </dd>
        <dt>Получение</dt>
        <dd>
          {deliveryLabels[order.deliveryMethod] || order.deliveryMethod}
          {order.address && (
            <>
              <br />
              <small>{order.address}</small>
            </>
          )}
        </dd>
        {order.trackNumber && (
          <>
            <dt>Трек-номер</dt>
            <dd>{order.trackNumber}</dd>
          </>
        )}
        <dt>Как платит</dt>
        <dd>
          {paymentMethodLabels[order.paymentMethod || ""] ||
            (order.paymentMethod === "online"
              ? "Онлайн на сайте"
              : order.paymentMethod || "Не указано")}
        </dd>
        {order.repackRequested && (
          <>
            <dt>Просьба</dt>
            <dd>Собрать всё в одну коробку</dd>
          </>
        )}
        {order.comment && (
          <>
            <dt>Комментарий</dt>
            <dd className="order-customer-comment">{order.comment}</dd>
          </>
        )}
      </dl>
    </section>
  );

  return (
    <>
      <header className="ws-drawer-head">
        <h2 tabIndex={-1} className="order-drawer-title">
          Заказ {order.orderNumber}
        </h2>
        <span className="ws-pill" data-tone={statusTone(order.status)}>
          {statusLabels[order.status] || order.status}
        </span>
        <button
          type="button"
          className="ws-drawer-close"
          aria-label="Закрыть заказ"
          onClick={onClose}
        >
          ×
        </button>
      </header>
      <div className="ws-drawer-body admin-order-workspace">
        {view === "summary" && (
          <>
            <div className="order-drawer-meta">
              <span>
                <time dateTime={order.createdAt}>
                  {dateFormat.format(new Date(order.createdAt))}
                </time>
                {" · "}
                <span className="order-payment-state" data-paid={order.paymentStatus === "paid"}>
                  {paymentLabel}
                </span>
              </span>
              <label className="order-status-control">
                Статус
                <select
                  aria-label={`Статус заказа ${order.orderNumber}`}
                  value={order.status}
                  disabled={closed || changingStatus || dirty}
                  onChange={(event) => void changeStatus(event.target.value)}
                >
                  {orderStatuses.map((status) => (
                    <option key={status} value={status}>
                      {statusLabels[status]}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            {changingStatus && <p role="status">Обновляем статус…</p>}
            {reasons.length > 0 && (
              <ul className="ws-todo" aria-label="Что нужно сделать">
                {reasons.map((reason) => (
                  <li key={reason.title}>
                    <span>
                      <strong>{reason.title}</strong>
                      {[
                        "Проверить отправление СДЭК",
                        "СДЭК ожидает повторной попытки",
                        "Уточнить результат оплаты",
                      ].includes(reason.title) && <p>{reason.text}</p>}
                    </span>
                    {reason.view && (
                      <button
                        type="button"
                        className="ws-button"
                        onClick={() => setView(reason.view!)}
                      >
                        Сделать
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
        <AdminOrderEditor
          canRefund={canRefund}
          order={order}
          onSaved={onSaved}
          onError={onError}
          registerBeforeLeave={registerBeforeLeave}
          onDirtyChange={setDirty}
          onReviewState={setReview}
          disabled={changingStatus}
          view={view}
          onViewChange={setView}
          customerCard={customerCard}
        />
      </div>
    </>
  );
}

function statusTone(status: string) {
  if (status === "new") return "attention";
  if (["paid", "confirmed", "completed"].includes(status)) return "ok";
  if (["assembling", "packed", "processing", "shipped"].includes(status)) return "progress";
  return undefined;
}
