import { chatHref } from "./lib/chat";
import { useCallback, useEffect, useState } from "react";
import { StoreHeader } from "./StoreHeader";
import { money } from "./lib/money";
import "./styles/order-detail.css";

type StoreUser = {
  fullName: string;
  lastName: string;
  phone: string;
  email: string;
};

export type OrderDetail = {
  orderNumber: string;
  deliveryMethod: string;
  address: string;
  comment: string;
  cancellationReason?: string;
  customerName: string;
  phone: string;
  email: string;
  status: string;
  paymentStatus: string;
  paymentAttemptStatus?: string;
  paymentMethod: string;
  deliveryFee: number;
  deliveryPayee?: "shop" | "carrier";
  trackNumber?: string;
  hasPreorder?: boolean;
  deliveryFeePending?: boolean;
  repackRequested?: boolean;
  subtotal: number;
  discountAmount?: number;
  total: number;
  paidAmount: number;
  refundedAmount: number;
  amountDue: number;
  paymentReady: boolean;
  createdAt: string;
  items: Array<{ productName: string; unitPrice: number; quantity: number }>;
  shipmentOffers: ShipmentOffer[];
};

type ShipmentOffer = {
  id: number;
  paymentToken?: string;
  status: string;
  deliveryFee: number;
  deliveryPayee?: "shop" | "carrier";
  subtotal: number;
  total: number;
  notifiedAt?: string;
  expiresAt?: string;
  boxes: number;
  items: Array<{
    productName: string;
    unitPrice: number;
    originalUnitPrice?: number;
    quantity: number;
  }>;
};

const orderStatusLabels: Record<string, string> = {
  new: "Новый",
  confirmed: "Подтверждён",
  assembling: "Собирается",
  processing: "Собирается",
  ready: "Готов к выдаче",
  shipped: "В доставке",
  completed: "Выполнен",
  cancelled: "Отменён",
};

const deliveryLabels: Record<string, string> = {
  pickup: "Самовывоз в Рязани",
  courier: "Курьер по Рязани",
  cdek: "СДЭК по России",
  post: "Почта России",
};

const paymentLabels: Record<string, string> = {
  payment_provider_pending: "Ожидает подключения оплаты",
  pending: "Ожидает оплаты",
  paid: "Оплачен",
  partially_paid: "Частично оплачен",
  on_delivery: "Оплата при получении",
  invoice: "Счёт от менеджера",
  manager_confirmation: "После подтверждения менеджером",
  cancelled: "Оплата отменена",
  refunded: "Возвращён",
};

const deliveryPendingText = (order: OrderDetail) =>
  order.deliveryPayee === "carrier"
    ? order.repackRequested
      ? "Менеджер проверит упаковку и уточнит стоимость доставки до отправки. Доставку вы оплатите перевозчику при получении."
      : "Менеджер уточнит стоимость доставки до отправки. Доставку вы оплатите перевозчику при получении."
    : "Менеджер уточнит стоимость доставки и сообщит итоговую сумму заказа до оплаты.";

export default function AccountOrderPage({ orderNumber }: { orderNumber: string }) {
  const isPreview = import.meta.env.DEV && orderNumber === "LOCAL-1024";
  const [user, setUser] = useState<StoreUser | null>(null);
  const [order, setOrder] = useState<OrderDetail | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [paying, setPaying] = useState(false);

  const payOffer = async (offer: ShipmentOffer) => {
    if (isPreview || order?.status === "cancelled" || !offer.paymentToken) return;
    setPaying(true);
    setError("");
    try {
      const response = await fetch(
        `/api/v1/payments/shipment-offers/${encodeURIComponent(offer.paymentToken)}`,
        { method: "POST", credentials: "same-origin" },
      );
      const body = (await response.json()) as { confirmationUrl?: string; error?: string };
      if (!response.ok || !body.confirmationUrl)
        throw new Error(body.error || "Не удалось начать оплату растений");
      window.location.assign(body.confirmationUrl);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Не удалось начать оплату растений");
      setPaying(false);
    }
  };

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      if (import.meta.env.DEV && orderNumber === "LOCAL-1024") {
        const { getPreviewOrder } = await import("./previewOrder");
        const previewOrder = getPreviewOrder(
          new URLSearchParams(window.location.search).get("state"),
        );
        setOrder(previewOrder);
        setUser({
          fullName: "Анна",
          lastName: "",
          phone: previewOrder.phone,
          email: previewOrder.email,
        });
        return;
      }
      const [meResponse, orderResponse] = await Promise.all([
        fetch("/api/v1/auth/me", { credentials: "same-origin", cache: "no-store" }),
        fetch(`/api/v1/account/orders/${encodeURIComponent(orderNumber)}`, {
          credentials: "same-origin",
          cache: "no-store",
        }),
      ]);
      if (meResponse.status === 401 || orderResponse.status === 401) {
        window.location.assign(`/login?returnTo=${encodeURIComponent(window.location.pathname)}`);
        return;
      }
      const meBody = (await meResponse.json()) as { user?: StoreUser; error?: string };
      const orderBody = (await orderResponse.json()) as { order?: OrderDetail; error?: string };
      if (!meResponse.ok || !meBody.user)
        throw new Error(meBody.error || "Не удалось загрузить профиль");
      if (!orderResponse.ok || !orderBody.order)
        throw new Error(orderBody.error || "Заказ не найден");
      setUser(meBody.user);
      setOrder(orderBody.order);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Не удалось загрузить заказ");
    } finally {
      setLoading(false);
    }
  }, [orderNumber]);

  const refreshOrder = useCallback(async () => {
    try {
      const response = await fetch(`/api/v1/account/orders/${encodeURIComponent(orderNumber)}`, {
        credentials: "same-origin",
        cache: "no-store",
      });
      if (response.status === 401) {
        window.location.assign(`/login?returnTo=${encodeURIComponent(window.location.pathname)}`);
        return;
      }
      const body = (await response.json()) as { order?: OrderDetail };
      if (response.ok && body.order) {
        setOrder(body.order);
        setError("");
      }
    } catch {
      // Фоновая проверка не должна заменять рабочую карточку сетевой ошибкой.
      // Следующая проверка или фокус окна попробует снова.
    }
  }, [orderNumber]);

  useEffect(() => {
    void load();
  }, [load]);

  // Active orders keep receiving both payment and fulfilment updates.
  // Focus refreshes immediately; hidden tabs wait until the buyer returns.
  useEffect(() => {
    if (isPreview || !order || ["cancelled", "completed"].includes(order.status)) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") void refreshOrder();
    }, 8_000);
    const refreshOnFocus = () => {
      void refreshOrder();
    };
    window.addEventListener("focus", refreshOnFocus);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", refreshOnFocus);
    };
  }, [isPreview, order, refreshOrder]);

  const pay = async () => {
    if (isPreview) {
      window.location.assign("/checkout?preview=payment-success");
      return;
    }
    if (
      !order ||
      order.paymentMethod === "on_delivery" ||
      order.status === "cancelled" ||
      !order.paymentReady ||
      order.hasPreorder ||
      order.deliveryFeePending ||
      order.amountDue <= 0
    )
      return;
    setPaying(true);
    setError("");
    try {
      const response = await fetch(
        `/api/v1/payments/orders/${encodeURIComponent(order.orderNumber)}`,
        {
          method: "POST",
          credentials: "same-origin",
        },
      );
      const body = (await response.json()) as { confirmationUrl?: string; error?: string };
      if (!response.ok || !body.confirmationUrl) {
        throw new Error(body.error || "Не удалось начать оплату");
      }
      window.location.assign(body.confirmationUrl);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Не удалось начать оплату");
      setPaying(false);
    }
  };

  const cancelled = order?.status === "cancelled";
  const cashOnPickup = order?.deliveryMethod === "pickup" && order?.paymentMethod === "on_delivery";
  const needsConfirmation =
    !!order &&
    !["cancelled", "ready", "shipped", "completed"].includes(order.status) &&
    (order.hasPreorder ||
      order.deliveryFeePending ||
      (!order.paymentReady && !cashOnPickup && order.status === "new" && order.amountDue > 0));
  const canPay =
    !!order &&
    !cancelled &&
    !cashOnPickup &&
    order.paymentReady &&
    !order.hasPreorder &&
    !order.deliveryFeePending &&
    order.amountDue > 0;
  const pickup = order?.deliveryMethod === "pickup";
  const stages = pickup
    ? ["Оформлен", "Подтверждён", "Готов к выдаче", "Получен"]
    : ["Оформлен", "Подтверждён", "Собран", "Передан в доставку", "Получен"];
  const stageIndex = order
    ? ({
        new: 0,
        confirmed: 1,
        assembling: 1,
        processing: 1,
        ready: 2,
        shipped: pickup ? 2 : 3,
        completed: stages.length - 1,
      }[order.status] ?? 0)
    : 0;
  const statusTitle = needsConfirmation
    ? "Ждёт подтверждения"
    : order
      ? (orderStatusLabels[order.status] ?? order.status)
      : "";
  const statusText = !order
    ? ""
    : cancelled
      ? `${order.cancellationReason ? `${order.cancellationReason}. ` : ""}Заказ отменён. Если растения ещё нужны, оформите новый заказ по актуальным цене и наличию.`
      : needsConfirmation
        ? order.hasPreorder
          ? "Менеджер уточнит наличие растений и свяжется с вами. Оплата пока не требуется."
          : order.deliveryFeePending
            ? deliveryPendingText(order)
            : "Менеджер проверит детали заказа. После подтверждения здесь появится кнопка оплаты."
        : canPay && order.status === "confirmed"
          ? "Заказ готов к оплате. Проверьте состав и способ получения."
          : order.status === "completed"
            ? "Спасибо, что выбрали Фикусин!"
            : order.status === "shipped"
              ? "Заказ передан в доставку. Трек-номер, если он уже доступен, указан ниже."
              : order.status === "ready"
                ? pickup
                  ? "Растения готовы к выдаче в магазине."
                  : "Растения собраны и готовы к отправке."
                : order.status === "assembling" || order.status === "processing"
                  ? "Собираем и проверяем ваши растения."
                  : "Заказ принят. Здесь можно следить за его статусом и деталями.";

  return (
    <main className="account-page order-detail-page">
      <StoreHeader />
      <section className="account-shell">
        <aside className="account-sidebar">
          <h1>Личный кабинет</h1>
          {user && <p>{[user.fullName, user.lastName].filter(Boolean).join(" ")}</p>}
          <nav aria-label="Разделы личного кабинета">
            <a className="active" href="/account">
              <span>Мои заказы</span>
            </a>
            <a href="/account/profile">
              <span>Мои данные</span>
            </a>
            <a href="/account/favorites">
              <span>Избранное</span>
            </a>
            <a href="/account/reviews">
              <span>Мои отзывы</span>
            </a>
          </nav>
        </aside>
        <div className="account-content">
          <a className="order-detail-back" href="/account">
            <span aria-hidden="true">←</span> Все заказы
          </a>
          {isPreview && (
            <p className="order-preview-note">Демонстрационный заказ · локальный просмотр</p>
          )}
          {loading && (
            <p className="order-detail-loading" role="status">
              Загружаем заказ…
            </p>
          )}
          {error && (
            <div className="auth-error" role="alert">
              <p>{error}</p>
              {!order && (
                <button className="secondary-button" onClick={() => void load()}>
                  Попробовать снова
                </button>
              )}
            </div>
          )}
          {!loading && order && (
            <>
              <header className="order-detail-heading">
                <h2>
                  Заказ <span>№ {order.orderNumber}</span>
                </h2>
                <p>
                  Оформлен{" "}
                  {new Date(order.createdAt).toLocaleDateString("ru-RU", {
                    day: "numeric",
                    month: "long",
                    year: "numeric",
                  })}
                </p>
              </header>
              <section
                className="order-journey"
                data-cancelled={cancelled}
                aria-label="Статус заказа"
              >
                <div className="order-journey-copy" aria-live="polite" aria-atomic="true">
                  <h3>{statusTitle}</h3>
                  {!cancelled && (
                    <img
                      className="order-journey-art"
                      src="/assets/redesign/order-plant-parcel-studio-v2.webp"
                      alt=""
                      width={176}
                      height={176}
                    />
                  )}
                  <p>{statusText}</p>
                  {canPay && (
                    <a className="order-payment-jump" href="#order-payment">
                      Перейти к оплате{" "}
                      <svg aria-hidden="true" viewBox="0 0 20 20">
                        <path d="M4 10h12m-5-5 5 5-5 5" />
                      </svg>
                    </a>
                  )}
                </div>
                {!cancelled && (
                  <ol className="order-journey-steps" aria-label="Этапы заказа" tabIndex={0}>
                    {stages.map((label, index) => (
                      <li
                        key={label}
                        data-state={
                          index < stageIndex
                            ? "done"
                            : index === stageIndex
                              ? "current"
                              : "upcoming"
                        }
                        aria-current={index === stageIndex ? "step" : undefined}
                      >
                        <span className="order-journey-marker" aria-hidden="true">
                          {index < stageIndex ? (
                            <svg viewBox="0 0 20 20">
                              <path d="m5 10 3 3 7-7" />
                            </svg>
                          ) : (
                            index + 1
                          )}
                        </span>
                        <span>{label}</span>
                      </li>
                    ))}
                  </ol>
                )}
              </section>
              <div className="order-detail-layout">
                <div className="order-detail-main">
                  <section className="order-detail-plants" aria-labelledby="order-plants-title">
                    <header>
                      <h3 id="order-plants-title">Ваши растения</h3>
                      <span>
                        {order.items.reduce((count, item) => count + item.quantity, 0)} шт.
                      </span>
                    </header>
                    <div className="order-detail-table" role="table" aria-label="Состав заказа">
                      <div className="order-detail-table-head" role="row">
                        <span role="columnheader">Товар</span>
                        <span role="columnheader">Цена</span>
                        <span role="columnheader">Кол-во</span>
                        <span role="columnheader">Сумма</span>
                      </div>
                      {order.items.map((item, index) => (
                        <div
                          className="order-detail-item"
                          role="row"
                          key={`${item.productName}-${index}`}
                        >
                          <strong role="cell">{item.productName}</strong>
                          <span role="cell">
                            <small>Цена: </small>
                            {money.format(item.unitPrice)}
                          </span>
                          <span role="cell">
                            {item.quantity}
                            <small> шт.</small>
                          </span>
                          <b role="cell">{money.format(item.unitPrice * item.quantity)}</b>
                        </div>
                      ))}
                    </div>
                  </section>
                  {!!order.shipmentOffers?.some(
                    (offer) =>
                      order.status !== "cancelled" ||
                      ["paid", "shipping", "shipped", "ready", "completed"].includes(offer.status),
                  ) && (
                    <section className="account-shipment-offers">
                      <div>
                        <p className="eyebrow">Частичная отправка</p>
                        <h3>Готовы к отправке</h3>
                      </div>
                      {order.shipmentOffers
                        .filter(
                          (offer) =>
                            order.status !== "cancelled" ||
                            ["paid", "shipping", "shipped", "ready", "completed"].includes(
                              offer.status,
                            ),
                        )
                        .map((offer) => (
                          <article
                            className={`account-shipment-offer account-shipment-offer-${offer.status}`}
                            key={offer.id}
                          >
                            <div>
                              {offer.items.map((item, index) => (
                                <p key={`${item.productName}-${index}`}>
                                  <strong>{item.productName}</strong>
                                  <span>
                                    {item.quantity} × {money.format(item.unitPrice)}
                                  </span>
                                  {item.originalUnitPrice != null &&
                                    item.originalUnitPrice !== item.unitPrice && (
                                      <small>
                                        При оформлении: {money.format(item.originalUnitPrice)} ·
                                        цена обновлена при поступлении
                                      </small>
                                    )}
                                </p>
                              ))}
                            </div>
                            <div>
                              <span>Растения</span>
                              <strong>{money.format(offer.subtotal)}</strong>
                              <span>
                                {offer.deliveryPayee === "carrier"
                                  ? "Доставка, справочно"
                                  : "Доставка"}{" "}
                                · {offer.boxes} кор.
                              </span>
                              <strong>{money.format(offer.deliveryFee)}</strong>
                              <span>
                                {offer.deliveryPayee === "carrier"
                                  ? "К оплате за растения"
                                  : "К оплате"}
                              </span>
                              <strong>{money.format(offer.total)}</strong>
                            </div>
                            {offer.deliveryPayee === "carrier" && (
                              <p className="order-note">
                                Доставка оплачивается перевозчику при получении.
                              </p>
                            )}
                            {order.status !== "cancelled" &&
                              offer.status === "offered" &&
                              offer.paymentToken && (
                                <>
                                  <button
                                    className="primary-button"
                                    disabled={paying || isPreview}
                                    onClick={() => void payOffer(offer)}
                                  >
                                    {paying
                                      ? "Открываем оплату…"
                                      : `${offer.deliveryPayee === "carrier" ? "Оплатить растения" : "Оплатить отправку"} ${money.format(offer.total)}`}
                                  </button>
                                  {offer.expiresAt && (
                                    <small>
                                      Оплатите до{" "}
                                      {new Date(offer.expiresAt).toLocaleString("ru-RU")}. Наличие
                                      проверяется перед оплатой.
                                    </small>
                                  )}
                                </>
                              )}
                            {offer.status === "payment_pending" && (
                              <p className="order-note">
                                Платёж проверяется. Повторная ссылка не создаётся.
                              </p>
                            )}
                            {offer.status === "paid" && (
                              <p className="order-note">Эта отправка оплачена.</p>
                            )}
                            {offer.status === "expired" && (
                              <p className="order-note">
                                Срок предложения истёк. Ожидаемые растения остались в исходном
                                заказе.
                              </p>
                            )}
                            {offer.status === "stale" && (
                              <p className="order-note">
                                Состав или адрес изменился. Менеджер подготовит новое предложение.
                              </p>
                            )}
                          </article>
                        ))}
                      {order.status !== "cancelled" && order.hasPreorder && (
                        <p className="order-note">
                          Ожидаем поступления остальных растений. Они сохраняются в исходном заказе
                          и будут предложены отдельно.
                        </p>
                      )}
                    </section>
                  )}
                  <section
                    className="order-detail-receiving"
                    aria-labelledby="order-receiving-title"
                  >
                    <h3 id="order-receiving-title">Получение заказа</h3>
                    <dl>
                      <div>
                        <dt>Способ получения</dt>
                        <dd>{deliveryLabels[order.deliveryMethod] ?? order.deliveryMethod}</dd>
                      </div>
                      {order.address && (
                        <div>
                          <dt>Адрес</dt>
                          <dd>{order.address}</dd>
                        </div>
                      )}
                      {order.trackNumber && (
                        <div>
                          <dt>Трек-номер</dt>
                          <dd>{order.trackNumber}</dd>
                        </div>
                      )}
                      <div>
                        <dt>Получатель</dt>
                        <dd>
                          {order.customerName}
                          <br />
                          {order.phone}
                          {order.email && (
                            <>
                              <br />
                              {order.email}
                            </>
                          )}
                        </dd>
                      </div>
                      {order.comment && (
                        <div>
                          <dt>Комментарий</dt>
                          <dd>{order.comment}</dd>
                        </div>
                      )}
                    </dl>
                  </section>
                </div>
                <aside className="order-detail-summary" aria-labelledby="order-summary-title">
                  <h3 id="order-summary-title">Ваш заказ</h3>
                  <dl>
                    <div>
                      <dt>Стоимость товаров</dt>
                      <dd>{money.format(order.subtotal + (order.discountAmount ?? 0))}</dd>
                    </div>
                    {!!order.discountAmount && (
                      <div>
                        <dt>Скидка</dt>
                        <dd>−{money.format(order.discountAmount)}</dd>
                      </div>
                    )}
                    <div>
                      <dt>
                        {order.deliveryPayee === "carrier" ? "Доставка перевозчику" : "Доставка"}
                      </dt>
                      <dd>
                        {order.deliveryFeePending
                          ? cancelled
                            ? "Не рассчитана"
                            : "Уточнит менеджер"
                          : order.deliveryFee === 0 && pickup
                            ? "Бесплатно"
                            : money.format(order.deliveryFee)}
                      </dd>
                    </div>
                    <div className="order-detail-total">
                      <dt>
                        {cancelled
                          ? "Сумма отменённого заказа"
                          : order.deliveryPayee === "carrier"
                            ? "К оплате за растения"
                            : order.deliveryFeePending
                              ? "Предварительно"
                              : "Итого"}
                      </dt>
                      <dd>{money.format(order.total)}</dd>
                    </div>
                    {order.paidAmount > 0 && (
                      <div>
                        <dt>Оплачено</dt>
                        <dd>{money.format(order.paidAmount)}</dd>
                      </div>
                    )}
                    {order.refundedAmount > 0 && (
                      <div>
                        <dt>Возвращено</dt>
                        <dd>{money.format(order.refundedAmount)}</dd>
                      </div>
                    )}
                    {!cancelled && order.amountDue > 0 && order.paidAmount > 0 && (
                      <div>
                        <dt>К доплате</dt>
                        <dd>{money.format(order.amountDue)}</dd>
                      </div>
                    )}
                  </dl>
                  <div className="order-detail-payment" id="order-payment" tabIndex={-1}>
                    <span>
                      {cancelled
                        ? "Заказ отменён"
                        : order.amountDue === 0 && order.paymentStatus === "paid"
                          ? "Заказ полностью оплачен"
                          : cashOnPickup
                            ? "Наличными при получении"
                            : (paymentLabels[order.paymentStatus] ?? order.paymentStatus)}
                    </span>
                    {needsConfirmation && (
                      <p>
                        {cashOnPickup
                          ? "Оплата в магазине после подтверждения заказа."
                          : "Оплата откроется после подтверждения менеджером."}
                      </p>
                    )}
                    {!cancelled && order.deliveryFeePending && order.hasPreorder && (
                      <p>{deliveryPendingText(order)}</p>
                    )}
                    {!cancelled &&
                      !order.deliveryFeePending &&
                      order.deliveryPayee === "carrier" && (
                        <p>Доставка оплачивается перевозчику при получении.</p>
                      )}
                    {canPay && (
                      <button
                        className="primary-button"
                        disabled={paying}
                        onClick={() => void pay()}
                      >
                        <span>
                          {paying
                            ? "Открываем оплату…"
                            : `${order.deliveryPayee === "carrier" ? "Оплатить растения" : "Оплатить заказ"} ${money.format(order.amountDue)}`}
                        </span>
                        {!paying && (
                          <svg aria-hidden="true" viewBox="0 0 20 20">
                            <path d="M4 10h12m-5-5 5 5-5 5" />
                          </svg>
                        )}
                      </button>
                    )}
                  </div>
                  <a className="order-detail-help" href={chatHref(`Заказ ${orderNumber}`)}>
                    Задать вопрос о заказе <span aria-hidden="true">→</span>
                  </a>
                </aside>
              </div>
            </>
          )}
        </div>
      </section>
    </main>
  );
}
