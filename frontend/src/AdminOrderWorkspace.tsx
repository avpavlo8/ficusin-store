import { useEffect, useMemo, useState } from "react";
import { AdminOrderEditor, type OrderReviewState, type OrderView } from "./AdminOrderEditor";
import { orderStatuses, statusLabels } from "./adminShared";
import type { Order, OrderWorkflow } from "./adminTypes";
import "./styles/admin-order-workspace.css";

type Props = {
  canRefund?: boolean;
  order: Order;
  onSaved: (order: Order) => void;
  onError: (message: string) => void;
  onStatusChange: (order: Order, status: string) => Promise<void>;
  registerBeforeLeave?: (guard: () => Promise<boolean>) => () => void;
};

type OrderReason = { title: string; text?: string; view?: OrderView; code?: string };

const reasonPriority = (reason: OrderReason) => {
  if (reason.code === "realization_attention") return 0;
  if (reason.view === "items") return 1;
  if (reason.code === "realization_sync") return 2;
  if (reason.code === "payment_check" || reason.code === "cdek_review") return 3;
  if (reason.view === "delivery") return 4;
  return 5;
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
  // The adjustment loaded before a status mutation still describes the old
  // stage. Keep the PATCH response visible while the editor refreshes it.
  const workflow = review?.workflow?.status === order.status ? review.workflow : order.workflow;
  const currentStatus = workflow?.status || order.status;
  const closed = ["cancelled", "completed"].includes(currentStatus);
  const reasons = useMemo(() => {
    const result: OrderReason[] = [];
    if (workflow) {
      for (const blocker of workflow.blockers)
        result.push({ title: blocker.message, view: blocker.view, code: blocker.code });
    }
    if (!closed && !workflow) {
      if (review?.hasPreorder ?? order.hasPreorder)
        result.push({
          title: "Подтвердить наличие растений",
          view: "items",
          text: "Наличие части растений требует подтверждения. Проверьте актуальный остаток и согласуйте состав заказа.",
        });
      if (review?.deliveryFeePending ?? order.deliveryFeePending)
        result.push({
          title:
            order.deliveryMethod === "cdek" && review?.packagingDimensionsMissing
              ? "Заполните размеры коробки"
              : "Рассчитать доставку",
          view: "delivery",
        });
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
          code: "cdek_review",
          view: "delivery",
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
    if (
      workflow &&
      !closed &&
      review &&
      ["unknown", "retry", "manual_review"].includes(review.cdekCreateState)
    ) {
      const title =
        review.cdekCreateState === "unknown"
          ? "Уточняется создание отправления"
          : review.cdekCreateState === "retry"
            ? "СДЭК ожидает повторной попытки"
            : "Проверить отправление СДЭК";
      if (!result.some((reason) => reason.title === title))
        result.push({
          title,
          text: review.cdekLastError || "Проверяется результат регистрации отправления СДЭК.",
          view: "delivery",
        });
    }
    if (review?.paymentIssues.length)
      result.push({
        title: "Уточнить результат оплаты",
        code: "payment_check",
        view: "payment",
        text: review.paymentIssues.some((issue) => issue.needsReview)
          ? "Автопроверка остановлена. Сверьте платёж в кабинете ЮKassa."
          : "ЮKassa ещё не подтвердила результат. Проверка продолжается автоматически.",
      });
    return result.sort((a, b) => reasonPriority(a) - reasonPriority(b));
  }, [order, review, closed, workflow]);

  const changeStatus = async (status: string) => {
    setChangingStatus(true);
    try {
      await onStatusChange(order, status);
    } finally {
      setChangingStatus(false);
    }
  };

  // Следующий шаг заказа — главная зелёная кнопка под составом.
  const next = nextStep(order, workflow);

  const reason = reasons[0];
  const realization = reason?.code === "realization_attention" || reason?.code === "realization_sync";
  const reasonDetail =
    reason?.code === "realization_attention"
      ? canRefund
        ? "После сверки можно подтвердить доставку и выставить оплату."
        : "Попросите владельца проверить реализацию в СБИС."
      : reason?.code === "realization_sync"
        ? "После подтверждения состава можно завершить подготовку доставки и выставить оплату."
        : reason?.text;
  const realizationUrl = `/admin?${new URLSearchParams({
    section: "orders",
    view: "realizations",
    q: order.orderNumber,
  })}`;

  const nowBlock =
    currentStatus === "cancelled" ? (
      <section className="order-now is-muted" aria-label="Что нужно сделать">
        <h2>Заказ отменён</h2>
      </section>
    ) : reason ? (
      <section className="order-now order-next-step" aria-label="Что нужно сделать">
        <div>
          <h2>{reason.title}</h2>
          {reasonDetail && <p>{reasonDetail}</p>}
        </div>
        {realization && canRefund && (
          <a className="ws-button" href={realizationUrl} target="_blank" rel="noopener noreferrer">
            Открыть реализацию
          </a>
        )}
        {reason.view && reason.code !== "payment" && (
          <button type="button" className="ws-button" onClick={() => setView(reason.view!)}>
            {reason.view === "items"
              ? "Проверить состав"
              : reason.view === "delivery"
                ? "Открыть доставку"
                : "Проверить оплату"}
          </button>
        )}
      </section>
    ) : null;

  const canCancel = !["cancelled", "completed", "shipped", "shipping"].includes(currentStatus);
  const cancelOrder = () => {
    if (!window.confirm(`Отменить заказ ${order.orderNumber}? Покупатель получит уведомление.`))
      return;
    void changeStatus("cancelled");
  };
  const renderActions = (editTarget: OrderView | null) =>
    closed ? null : (
      <div className="order-actions">
        {canCancel && (
          <button
            type="button"
            className="ws-button order-cancel"
            disabled={changingStatus || dirty}
            onClick={cancelOrder}
          >
            Отменить заказ
          </button>
        )}
        <span className="order-actions-gap" />
        {editTarget && (
          <button type="button" className="ws-button" onClick={() => setView(editTarget)}>
            Изменить заказ
          </button>
        )}
        {next && (
          <button
            type="button"
            className="ws-button primary"
            disabled={changingStatus || dirty}
            onClick={() => void changeStatus(next.status)}
          >
            {changingStatus ? "Сохраняем…" : next.action}
          </button>
        )}
      </div>
    );

  const statusControl = (
    <section className="order-status-card">
      <label htmlFor={`order-status-${order.id}`}>Статус заказа</label>
      <select
        id={`order-status-${order.id}`}
        aria-label={`Статус заказа ${order.orderNumber}`}
        value={currentStatus}
        disabled={order.deliveryMethod !== "pickup" || closed || changingStatus || dirty}
        onChange={(event) => void changeStatus(event.target.value)}
      >
        {(workflow
          ? [currentStatus, ...workflow.manualStatuses.filter((status) => status !== currentStatus)]
          : orderStatuses
        ).map((status) => (
          <option key={status} value={status}>
            {statusLabels[status]}
          </option>
        ))}
      </select>
      {order.deliveryMethod === "cdek" && !closed && (
        <small>Переход на сборку и этапы СДЭК обновляются автоматически.</small>
      )}
      {changingStatus && <small role="status">Обновляем статус…</small>}
    </section>
  );

  return (
    <div className="admin-order-workspace">
      <header className="order-page-head">
        <div className="order-page-title">
          <h2 tabIndex={-1} className="order-drawer-title">
            Заказ {order.orderNumber}
          </h2>
          <span className="ws-pill" data-tone={statusTone(currentStatus)}>
            {statusLabels[currentStatus] || currentStatus}
          </span>
        </div>
        <p>
          <time dateTime={order.createdAt}>{dateFormat.format(new Date(order.createdAt))}</time>
        </p>
      </header>
      <AdminOrderEditor
        canRefund={canRefund}
        order={{ ...order, status: currentStatus, workflow }}
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
        renderActions={renderActions}
      />
    </div>
  );
}

// Следующий обычный шаг по статусу — одна кнопка вместо поиска нужного
// пункта в списке статусов.
function nextStep(order: Order, workflow?: OrderWorkflow) {
  if (order.deliveryMethod !== "pickup") return null;
  const status = workflow?.status || order.status;
  const allowed =
    workflow?.manualStatuses ||
    (["new", "confirmed"].includes(status)
      ? ["assembling"]
      : status === "assembling"
        ? ["ready"]
        : status === "ready"
          ? ["completed"]
          : []);
  if (allowed.includes("assembling")) return { status: "assembling", action: "Начать сборку" };
  if (allowed.includes("ready")) return { status: "ready", action: "Заказ собран" };
  if (allowed.includes("completed")) return { status: "completed", action: "Заказ выдан" };
  return null;
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
