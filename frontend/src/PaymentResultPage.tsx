import { useEffect, useState } from "react";
import { StoreHeader } from "./StoreHeader";
import { money } from "./lib/money";
import { netOrderPayment, paymentResultState, readPaymentResultOrder, type PaymentResultOrder } from "./lib/payment-result";
import { reportPaidOrder } from "./lib/metrika-payment";
import "./styles/payment-result.css";

type ResultView =
  | { kind: "loading" }
  | { kind: "auth" }
  | { kind: "error"; message: string }
  | { kind: "order"; order: PaymentResultOrder; autoChecking: boolean; checkError?: string };

const deliveryNames: Record<string, string> = {
  pickup: "Самовывоз в Рязани", cdek: "СДЭК по России", courier: "Курьер по Рязани", post: "Почта России",
};

function ResultArrow({ diagonal = false }: { diagonal?: boolean }) {
  return <svg viewBox="0 0 20 20" aria-hidden="true"><path d={diagonal ? "M5 15 15 5M5 5h10v10" : "M4 10h12m-5-5 5 5-5 5"} /></svg>;
}

export default function PaymentResultPage({ orderNumber, preview }: { orderNumber: string; preview?: "success" | "pending" | "failed" }) {
  const localPreview = import.meta.env.DEV ? preview : undefined;
  const [view, setView] = useState<ResultView>({ kind: "loading" });
  const [retry, setRetry] = useState(0);
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempts = 0;
    const controller = new AbortController();
    setChecking(true);

    async function checkPayment() {
      try {
        if (import.meta.env.DEV && localPreview) {
          const { getPreviewOrder } = await import("./previewOrder");
          if (!disposed) setView({ kind: "order", order: getPreviewOrder(localPreview === "success" ? "paid" : localPreview === "failed" ? "payment-failed" : "confirmed"), autoChecking: false });
          return;
        }
        if (attempts === 0) {
          const auth = await fetch("/api/v1/auth/me", { credentials: "same-origin", cache: "no-store", signal: controller.signal });
          if (disposed) return;
          if (auth.status === 401) { setView({ kind: "auth" }); return; }
          if (!auth.ok) throw new Error("Не удалось проверить вход в личный кабинет.");
        }
        attempts += 1;
        const response = await fetch(`/api/v1/account/orders/${encodeURIComponent(orderNumber)}`, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
        if (disposed) return;
        if (response.status === 401) { setView({ kind: "auth" }); return; }
        if (!response.ok) throw new Error(response.status === 404 ? "Заказ не найден в вашем личном кабинете." : "Не удалось получить статус оплаты.");
        const body = await response.json() as { order?: unknown };
        if (disposed) return;
        const order = readPaymentResultOrder(body.order, orderNumber);
        if (!order) throw new Error("Не удалось проверить данные оплаты.");
        void reportPaidOrder(order);
        const autoChecking = ["pending", "partial"].includes(paymentResultState(order)) && attempts < 20;
        setView({ kind: "order", order, autoChecking });
        if (autoChecking) timer = setTimeout(() => void checkPayment(), 3000);
      } catch (reason) {
        if (!disposed) {
          const message = reason instanceof Error ? reason.message : "Не удалось проверить оплату.";
          setView((current) => current.kind === "order" ? { ...current, autoChecking: false, checkError: message } : { kind: "error", message });
        }
      } finally {
        if (!disposed) setChecking(false);
      }
    }
    void checkPayment();
    return () => { disposed = true; controller.abort(); if (timer) clearTimeout(timer); };
  }, [localPreview, orderNumber, retry]);

  const order = view.kind === "order" ? view.order : null;
  const state = view.kind === "order" ? paymentResultState(view.order) : view.kind;
  const success = state === "success";
  const pending = state === "pending";
  const failed = state === "failed";
  const illustrated = success || pending || failed;
  const orderHref = `/account/orders/${encodeURIComponent(orderNumber)}${localPreview ? localPreview === "success" ? "?state=paid" : "?state=confirmed" : ""}`;
  const loginHref = `/login?returnTo=${encodeURIComponent(window.location.pathname + window.location.search)}`;
  const title = {
    success: "Оплата прошла",
    pending: "Проверяем оплату",
    failed: "Оплата не прошла",
    partial: "Частичная оплата",
    cancelled: "Заказ отменён",
    refunded: "Оплата возвращена",
    unconfirmed: "Оплата не подтверждена",
    loading: "Проверяем оплату",
    auth: "Проверим оплату после входа",
    error: "Не удалось проверить оплату",
  }[state];
  const description = {
    success: "Спасибо! Оплата вашего заказа подтверждена.",
    pending: "Подтверждение ещё не поступило. Если деньги списались, не оплачивайте заказ повторно — дождитесь обновления статуса.",
    failed: "Платёж не завершён. Заказ сохранён — вернуться к оплате можно в его карточке.",
    partial: "По заказу учтена частичная оплата. Актуальный остаток доступен в карточке заказа.",
    cancelled: "Посмотрите состояние оплаты и возврата в карточке заказа.",
    refunded: "Подробности возврата доступны в карточке заказа.",
    unconfirmed: "Посмотрите актуальный статус и доступные действия в карточке заказа.",
    loading: "Получаем актуальный статус вашего заказа.",
    auth: "Войдите в личный кабинет, чтобы увидеть подтверждённый статус и сумму оплаты.",
    error: view.kind === "error" ? view.message : "Попробуйте проверить статус ещё раз.",
  }[state];

  function checkAgain() { setChecking(true); setRetry((value) => value + 1); }

  return <main className="payment-result-page">
    <StoreHeader />
    <section className="payment-result-shell" aria-labelledby="payment-result-title">
      {localPreview && <p className="payment-result-preview">Демонстрационный экран · локальный просмотр</p>}
      <article className="payment-result-card" data-success={success} data-failed={failed} data-illustrated={illustrated} aria-busy={state === "loading"}>
        {illustrated && <img className="payment-result-art" src="/assets/redesign/order-plant-parcel-studio-v2.webp" width={420} height={420} alt="" />}
        <div className="payment-result-copy">
          <svg className="payment-result-mark" viewBox="0 0 48 48" aria-hidden="true"><circle cx="24" cy="24" r="22" />{success ? <path d="m14 24 7 7 14-15" /> : failed ? <path d="M24 13v14m0 7v1" /> : <path d="M24 12v13l8 5" />}</svg>
          <div className="payment-result-message" aria-live="polite" aria-atomic="true">
            <h1 id="payment-result-title">{title}</h1>
            <p className="payment-result-lead">{description}</p>
          </div>
          {order && <dl className="payment-result-facts">
            <div><dt>Заказ</dt><dd>№ {order.orderNumber}</dd></div>
            {(pending || failed) && <div><dt>Сумма заказа</dt><dd className="payment-result-amount">{money.format(order.total)}</dd></div>}
            {(success || state === "partial") && <div><dt>Оплачено по заказу</dt><dd className="payment-result-amount">{money.format(netOrderPayment(order))}</dd></div>}
            {state === "refunded" && <div><dt>Возвращено</dt><dd className="payment-result-amount">{money.format(order.refundedAmount)}</dd></div>}
            {success && <div><dt>Получение</dt><dd>{deliveryNames[order.deliveryMethod] ?? order.deliveryMethod}</dd></div>}
          </dl>}
          {success && <p className="payment-result-next">Следить за сборкой и получением растений можно в заказе.</p>}
          {pending && <p className="payment-result-next" role="status">{view.kind === "order" && view.autoChecking ? "Проверяем статус автоматически." : "Проверьте статус чуть позже — здесь или в заказе."}</p>}
          {failed && <p className="payment-result-next">Если деньги списались, проверьте статус в заказе, прежде чем повторять оплату.</p>}
          {view.kind === "order" && view.checkError && <p className="payment-result-check-error" role="alert">{view.checkError} Попробуйте ещё раз.</p>}
          <div className="payment-result-actions">
            {view.kind === "auth" ? <a className="payment-result-primary" href={loginHref}>Войти и проверить <ResultArrow /></a>
              : state === "error" || pending ? <button className="payment-result-primary" onClick={checkAgain} disabled={checking}>{checking ? "Проверяем…" : "Проверить ещё раз"} <ResultArrow /></button>
                : state !== "loading" && <a className="payment-result-primary" href={orderHref}>{success ? "Следить за заказом" : failed ? "Вернуться к заказу" : "Открыть заказ"}<ResultArrow /></a>}
            <a className="payment-result-catalog" href={pending ? orderHref : "/#catalog"}>{pending ? "Открыть заказ" : "Вернуться в каталог"} <ResultArrow diagonal /></a>
          </div>
        </div>
      </article>
    </section>
  </main>;
}
