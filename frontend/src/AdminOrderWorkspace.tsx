import { useMemo, useState } from "react";
import { AdminOrderEditor, type OrderReviewState } from "./AdminOrderEditor";
import { money, orderStatuses, paymentLabels, paymentMethodLabels, statusLabels } from "./adminShared";
import type { Order } from "./adminTypes";
import { WorkspaceStatus } from "./WorkspaceUI";
import "./styles/admin-order-workspace.css";

type Props = {
  order: Order;
  onSaved: (order: Order) => void;
  onError: (message: string) => void;
  onStatusChange: (order: Order, status: string) => Promise<void>;
  registerBeforeLeave?: (guard: () => Promise<boolean>) => () => void;
};

const dateFormat = new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric", hour: "2-digit", minute: "2-digit" });
const deliveryLabels: Record<string, string> = { pickup: "Самовывоз", cdek: "СДЭК", courier: "Курьер" };

export function AdminOrderWorkspace({ order, onSaved, onError, onStatusChange, registerBeforeLeave }: Props) {
  const [review, setReview] = useState<OrderReviewState | null>(null);
  const [dirty, setDirty] = useState(false);
  const [changingStatus, setChangingStatus] = useState(false);
  const closed = ["cancelled", "completed"].includes(order.status);
  const reasons = useMemo(() => {
    const result: Array<{ title: string; text: string }> = [];
    if (!closed) {
      if (review?.hasPreorder ?? order.hasPreorder) result.push({ title: "Подтвердить наличие растений", text: "Наличие части растений требует подтверждения. Проверьте актуальный остаток и согласуйте состав заказа." });
      if (review?.deliveryFeePending ?? order.deliveryFeePending) result.push(order.repackRequested
        ? { title: "Собрать в одну коробку и рассчитать доставку", text: "Покупатель просит объединить растения в одну коробку. Проверьте упаковку и стоимость доставки." }
        : order.deliveryMethod === "cdek" && review?.packagingDimensionsMissing
          ? { title: "Заполните размеры коробки", text: "Для расчёта доставки не хватает размеров упаковки. Укажите фактические размеры и вес коробки ниже." }
        : { title: "Рассчитать доставку", text: "Стоимость доставки не рассчитана. Проверьте упаковку и уточните стоимость." });
      if (order.paymentMethod === "manager_confirmation") result.push({ title: "Подтвердить заказ перед оплатой", text: "Выбран способ оплаты после подтверждения менеджером." });
      if (order.paymentMethod === "invoice") result.push({ title: "Подготовить счёт", text: "Покупатель выбрал оплату по счёту на организацию." });
      if (order.deliveryMethod === "cdek" && review?.cdekCreateState === "manual_review") result.push({ title: "Проверить отправление СДЭК", text: review.cdekLastError || "Нужно сверить отправление в кабинете СДЭК. Новая заявка автоматически не создаётся." });
      if (order.deliveryMethod === "cdek" && review?.cdekCreateState === "unknown") result.push({ title: "Уточняется создание отправления", text: "СДЭК не подтвердил результат. Система ищет заявку по номеру заказа без повторного создания." });
      if (order.deliveryMethod === "cdek" && review?.cdekCreateState === "retry") result.push({ title: "СДЭК ожидает повторной попытки", text: review.cdekLastError || "Предыдущая попытка создания отправления не завершилась успешно." });
    }
    if (review?.paymentIssues.length) result.push({ title: "Уточнить результат оплаты", text: review.paymentIssues.some((issue) => issue.needsReview)
      ? "Автопроверка остановлена. Сверьте платёж с кабинетом ЮKassa в разделе «Оплата»."
      : "ЮKassa ещё не подтвердила результат. Проверка продолжается; подробности — в разделе «Оплата»." });
    return result;
  }, [order, review, closed]);
  const paymentLabel = order.status === "cancelled" && ["pending", "manager_confirmation", "payment_provider_pending", "on_delivery"].includes(order.paymentStatus)
    ? "Оплата не требуется"
    : order.paymentStatus === "partially_paid" ? "Частично оплачен" : paymentLabels[order.paymentStatus] || order.paymentStatus;

  const changeStatus = async (status: string) => {
    setChangingStatus(true);
    try { await onStatusChange(order, status); }
    finally { setChangingStatus(false); }
  };

  return <div className="admin-order-workspace">
    <header className="order-workspace-header">
      <div className="order-workspace-title">
        <div><h2 tabIndex={-1}>Заказ {order.orderNumber}</h2><WorkspaceStatus status={order.status}>{statusLabels[order.status] || order.status}</WorkspaceStatus></div>
        <p><time dateTime={order.createdAt}>{dateFormat.format(new Date(order.createdAt))}</time><span>{money.format(order.total)}</span><span>{paymentLabel}</span></p>
      </div>
      <label className="order-workspace-status">Статус заказа
        <select aria-label={`Статус заказа ${order.orderNumber}`} value={order.status} disabled={closed || changingStatus || dirty} onChange={(event) => void changeStatus(event.target.value)}>
          {orderStatuses.map((status) => <option key={status} value={status}>{statusLabels[status]}</option>)}
        </select>
        {dirty && <small>Сначала завершите работу с изменениями заказа</small>}
        {changingStatus && <small role="status">Обновляем статус…</small>}
      </label>
    </header>

    <div className="order-workspace-body">
      {reasons.length > 0 && <section className="order-workspace-attention" aria-label="Почему требуется менеджер">
        <ul>{reasons.map((reason) => <li key={reason.title}><strong>{reason.title}</strong>{["Проверить отправление СДЭК", "СДЭК ожидает повторной попытки", "Уточнить результат оплаты"].includes(reason.title) && <p>{reason.text}</p>}</li>)}</ul>
      </section>}

      <aside className="order-workspace-sidebar" aria-label="Данные заказа">
        <section className="order-workspace-items"><h3>В заказе</h3><ul>{order.items.map((item, index) => <li key={`${item.sku}-${index}`}><span>{item.productName}<small>{item.quantity} шт. × {money.format(item.unitPrice)}</small></span><strong>{money.format(item.unitPrice * item.quantity)}</strong></li>)}</ul><a href="#order-composition" onClick={(event) => { event.preventDefault(); const heading = document.getElementById("order-composition"); heading?.scrollIntoView({ block: "start" }); heading?.focus({ preventScroll: true }); }}>{closed || order.status === "shipped" ? "Посмотреть состав" : "Изменить состав"}</a></section>
        <section><h3>Покупатель</h3><strong className="order-customer-name">{order.customerName || "Имя не указано"}</strong>
          {order.phone ? <a href={`tel:${order.phone}`}>{order.phone}</a> : <p>Телефон не указан</p>}
          {order.email && <a href={`mailto:${order.email}`}>{order.email}</a>}
        </section>
        <section><h3>Получение</h3><strong>{deliveryLabels[order.deliveryMethod] || order.deliveryMethod}</strong>
          {order.address && <p>{order.address}</p>}
          {order.trackNumber && <dl><dt>Трек-номер</dt><dd>{order.trackNumber}</dd></dl>}
          {order.repackRequested && <p className="order-customer-request">Просьба покупателя: одна коробка</p>}
        </section>
        <section><h3>Способ оплаты</h3><p>{paymentMethodLabels[order.paymentMethod || ""] || (order.paymentMethod === "online" ? "Онлайн на сайте" : order.paymentMethod || "Не указан")}</p><span className="order-payment-state" data-paid={order.paymentStatus === "paid"}>{paymentLabel}</span></section>
        {order.comment && <section><h3>Комментарий покупателя</h3><p className="order-customer-comment">{order.comment}</p></section>}
      </aside>

      <AdminOrderEditor order={order} onSaved={onSaved} onError={onError} registerBeforeLeave={registerBeforeLeave} onDirtyChange={setDirty} onReviewState={setReview} disabled={changingStatus} />
    </div>
  </div>;
}
