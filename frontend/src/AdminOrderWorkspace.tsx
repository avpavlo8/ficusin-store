import { useEffect, useMemo, useState } from "react";
import { AdminOrderEditor, type OrderReviewState, type OrderView } from "./AdminOrderEditor";
import { orderStatuses, paymentLabels, statusLabels } from "./adminShared";
import type { Order } from "./adminTypes";
import "./styles/admin-order-workspace.css";

type Props = {
  canRefund?: boolean;
  order: Order;
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

export function AdminOrderWorkspace({
  canRefund = false,
  order,
  onSaved,
  onError,
  onStatusChange,
  registerBeforeLeave,
}: Props) {
  const [review, setReview] = useState<OrderReviewState | null>(null);
  const [dirty, setDirty] = useState(false);
  const [changingStatus, setChangingStatus] = useState(false);
  // Режим правки живёт в адресе (&edit=items): кнопка «Назад» на телефоне
  // возвращает к просмотру заказа, а ссылку на нужный блок можно переслать.
  const [view, setViewState] = useState<OrderView>(readOrderView);
  const setView = (next: OrderView) => {
    const url = new URL(window.location.href);
    if (next === "summary") url.searchParams.delete("edit");
    else url.searchParams.set("edit", next);
    window.history.pushState(null, "", url);
    setViewState(next);
  };
  useEffect(() => {
    const sync = () => setViewState(readOrderView());
    window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, []);
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
                  text: "Для расчёта доставки не хватает размеров упаковки. Укажите фактические размеры и вес коробки — после этого посчитается доставка.",
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
    // Сначала то, что остановилось с ошибкой (СДЭК, оплата): это срочнее
    // обычных шагов вроде расчёта доставки.
    const urgent = [
      "Проверить отправление СДЭК",
      "СДЭК ожидает повторной попытки",
      "Уточнить результат оплаты",
    ];
    return result.sort(
      (a, b) => Number(urgent.includes(b.title)) - Number(urgent.includes(a.title)),
    );
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

  // Путь заказа: где он сейчас и что впереди. Отменённый заказ путь не
  // показывает — вместо него одна понятная плашка.
  const path = ["new", "confirmed", "assembling", "ready", "shipped", "completed"];
  const step = path.indexOf(order.status);
  const next = nextStep(order);

  const nowBlock =
    order.status === "cancelled" ? (
      <section className="order-now is-muted" aria-label="Что нужно сделать">
        <h2>Заказ отменён</h2>
        <p>Ничего делать не нужно. Состав и оплата сохранены для истории.</p>
      </section>
    ) : reasons.length > 0 ? (
      <section className="order-now" aria-label="Что нужно сделать">
        <span className="order-now-label">Что сделать сейчас</span>
        <h2>{reasons[0].title}</h2>
        <p>{reasons[0].text}</p>
        <div className="order-now-actions">
          {reasons[0].view && (
            <button
              type="button"
              className="ws-button primary"
              onClick={() => setView(reasons[0].view!)}
            >
              Сделать
            </button>
          )}
          {order.phone && (
            <a className="ws-button" href={`tel:${order.phone}`}>
              Позвонить покупателю
            </a>
          )}
        </div>
        {reasons.length > 1 && (
          <ul className="order-now-more">
            {reasons.slice(1).map((reason) => (
              <li key={reason.title}>
                <span>{reason.title}</span>
                {reason.view && (
                  <button
                    type="button"
                    className="ws-link-button"
                    onClick={() => setView(reason.view!)}
                  >
                    Сделать
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
    ) : next ? (
      <section className="order-now" aria-label="Что нужно сделать">
        <span className="order-now-label">Что сделать сейчас</span>
        <h2>{next.title}</h2>
        <p>{next.text}</p>
        <div className="order-now-actions">
          <button
            type="button"
            className="ws-button primary"
            disabled={changingStatus || dirty}
            onClick={() => void changeStatus(next.status)}
          >
            {changingStatus ? "Сохраняем…" : next.action}
          </button>
          {order.phone && (
            <a className="ws-button" href={`tel:${order.phone}`}>
              Позвонить покупателю
            </a>
          )}
        </div>
      </section>
    ) : null;

  const statusControl = (
    <label className="order-status-control">
      Статус вручную
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
  );

  return (
    <div className="admin-order-workspace">
      <header className="order-page-head">
        <div className="order-page-title">
          <h2 tabIndex={-1} className="order-drawer-title">
            Заказ {order.orderNumber}
          </h2>
          <span className="ws-pill" data-tone={statusTone(order.status)}>
            {statusLabels[order.status] || order.status}
          </span>
        </div>
        <p>
          <time dateTime={order.createdAt}>{dateFormat.format(new Date(order.createdAt))}</time>
          {" · "}
          <span className="order-payment-state" data-paid={order.paymentStatus === "paid"}>
            {paymentLabel}
          </span>
        </p>
        {order.status !== "cancelled" && view === "summary" && (
          <ol className="order-path" aria-label="Путь заказа">
            {path.map((status, index) => (
              <li
                key={status}
                data-state={index < step ? "done" : index === step ? "current" : "next"}
                aria-current={index === step ? "step" : undefined}
              >
                <span aria-hidden="true" />
                {statusLabels[status]}
              </li>
            ))}
          </ol>
        )}
        {changingStatus && <p role="status">Обновляем статус…</p>}
      </header>
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
        nowBlock={nowBlock}
        statusControl={statusControl}
      />
    </div>
  );
}

// Следующий обычный шаг по статусу — одна кнопка вместо поиска нужного
// пункта в списке статусов.
function nextStep(order: Order) {
  const pickup = order.deliveryMethod === "pickup";
  switch (order.status) {
    case "new":
      return {
        status: "confirmed",
        title: "Подтвердите заказ",
        text: "Проверьте наличие растений и подтвердите заказ — покупатель получит уведомление.",
        action: "Подтвердить заказ",
      };
    case "confirmed":
      return {
        status: "assembling",
        title: "Соберите растения",
        text: "Возьмите растения по списку «Что собрать» и отметьте, что начали сборку.",
        action: "Начать сборку",
      };
    case "assembling":
      return {
        status: "ready",
        title: "Закончите сборку",
        text: "Когда всё упаковано, отметьте заказ готовым.",
        action: "Заказ собран",
      };
    case "ready":
      return pickup
        ? {
            status: "completed",
            title: "Выдайте заказ покупателю",
            text: "Покупатель заберёт заказ сам. После выдачи отметьте заказ завершённым.",
            action: "Заказ выдан",
          }
        : {
            status: "shipped",
            title: "Передайте заказ в доставку",
            text: "Отдайте коробки курьеру или в пункт приёма и отметьте это здесь.",
            action: "Передан в доставку",
          };
    case "shipped":
      return {
        status: "completed",
        title: "Ждём, когда покупатель получит",
        text: "Когда заказ получен, отметьте его завершённым.",
        action: "Заказ получен",
      };
    default:
      return null;
  }
}

function statusTone(status: string) {
  if (status === "new") return "attention";
  if (["paid", "confirmed", "completed"].includes(status)) return "ok";
  if (["assembling", "packed", "processing", "shipped"].includes(status)) return "progress";
  return undefined;
}

const orderViews: OrderView[] = ["summary", "items", "delivery", "payment", "shipments"];
function readOrderView(): OrderView {
  const value = new URLSearchParams(window.location.search).get("edit") as OrderView | null;
  return value && orderViews.includes(value) ? value : "summary";
}
