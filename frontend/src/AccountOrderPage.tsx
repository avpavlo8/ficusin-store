import { useCallback, useEffect, useState } from "react";
import { StoreHeader } from "./StoreHeader";

type StoreUser = {
  fullName: string;
  lastName: string;
  phone: string;
  email: string;
};

type OrderDetail = {
  orderNumber: string;
  deliveryMethod: string;
  address: string;
  comment: string;
  customerName: string;
  phone: string;
  email: string;
  status: string;
  paymentStatus: string;
  paymentMethod: string;
  deliveryFee: number;
  trackNumber?: string;
  hasPreorder?: boolean;
  deliveryFeePending?: boolean;
  repackRequested?: boolean;
  subtotal: number;
  total: number;
  paidAmount: number;
  refundedAmount: number;
  amountDue: number;
  paymentReady: boolean;
  createdAt: string;
  items: Array<{ productName: string; unitPrice: number; quantity: number }>;
};

const money = new Intl.NumberFormat("ru-RU", {
  style: "currency",
  currency: "RUB",
  maximumFractionDigits: 0,
});

const orderStatusLabels: Record<string, string> = {
  new: "Новый",
  confirmed: "Подтверждён",
  assembling: "Собирается",
  processing: "Собирается",
  ready: "Готов к выдаче",
  shipped: "Передан в доставку",
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

const deliveryPendingText = (order: OrderDetail) => order.repackRequested
  ? "Менеджер проверит упаковку и пересчитает доставку. До подтверждения итоговой суммы оплата закрыта."
  : "Менеджер уточнит стоимость доставки. До подтверждения итоговой суммы оплата закрыта.";

export default function AccountOrderPage({ orderNumber }: { orderNumber: string }) {
  const [user, setUser] = useState<StoreUser | null>(null);
  const [order, setOrder] = useState<OrderDetail | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [paying, setPaying] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
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
      const meBody = await meResponse.json() as { user?: StoreUser; error?: string };
      const orderBody = await orderResponse.json() as { order?: OrderDetail; error?: string };
      if (!meResponse.ok || !meBody.user) throw new Error(meBody.error || "Не удалось загрузить профиль");
      if (!orderResponse.ok || !orderBody.order) throw new Error(orderBody.error || "Заказ не найден");
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
      const body = await response.json() as { order?: OrderDetail };
      if (response.ok && body.order) {
        setOrder(body.order);
        setError("");
      }
    } catch {
      // Фоновая проверка не должна заменять рабочую карточку сетевой ошибкой.
      // Следующая проверка или фокус окна попробует снова.
    }
  }, [orderNumber]);

  useEffect(() => { void load(); }, [load]);

  // Клиент не редактирует заказ. Пока он ждёт решения менеджера, карточка
  // сама подхватывает подтверждение: после сохранения в админке кнопка оплаты
  // появляется без ручного F5. Фокус окна даёт мгновенную проверку, таймер —
  // запасной путь, если вкладка всё время открыта.
  useEffect(() => {
    if (!order || order.paymentReady || order.amountDue <= 0) return;
    const timer = window.setInterval(() => { void refreshOrder(); }, 8_000);
    const refreshOnFocus = () => { void refreshOrder(); };
    window.addEventListener("focus", refreshOnFocus);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", refreshOnFocus);
    };
  }, [order, refreshOrder]);

  const pay = async () => {
    if (!order || !order.paymentReady || order.amountDue <= 0) return;
    setPaying(true);
    setError("");
    try {
      const response = await fetch(`/api/v1/payments/orders/${encodeURIComponent(order.orderNumber)}`, {
        method: "POST",
        credentials: "same-origin",
      });
      const body = await response.json() as { confirmationUrl?: string; error?: string };
      if (!response.ok || !body.confirmationUrl) {
        throw new Error(body.error || "Не удалось начать оплату");
      }
      window.location.assign(body.confirmationUrl);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Не удалось начать оплату");
      setPaying(false);
    }
  };

  return <main className="account-page">
    <StoreHeader />
    <section className="account-shell">
      <aside className="account-sidebar">
        <h1>{user ? [user.lastName, user.fullName].filter(Boolean).join(" ") : "Личный кабинет"}</h1>
        {user && <p>{user.phone}{user.email ? <><br />{user.email}</> : null}</p>}
        <nav aria-label="Разделы личного кабинета">
          <a className="active" href="/account"><span>Мои заказы</span></a>
          <a href="/account/profile"><span>Мои данные</span></a>
          <a href="/account/favorites"><span>Избранное</span></a>
          <a href="/account/reviews"><span>Мои отзывы</span></a>
        </nav>
      </aside>
      <div className="account-content">
        <a className="text-link account-back-link" href="/account">← Ко всем заказам</a>
        {loading && <div className="account-title"><div><p className="eyebrow">Заказ</p><h2>Загружаем…</h2></div></div>}
        {error && <p className="auth-error" role="alert">{error}</p>}
        {!loading && order && <>
          <div className="account-title">
            <div>
              <p className="eyebrow">Оформлен {new Date(order.createdAt).toLocaleDateString("ru-RU")}</p>
              <h2>Заказ {order.orderNumber}</h2>
            </div>
            <span>{orderStatusLabels[order.status] ?? order.status}</span>
          </div>

          <section className="order-items">
            {order.items.map((item, index) => <div className="order-item" key={`${item.productName}-${index}`}>
              <span>{item.productName}</span>
              <small>{item.quantity} × {money.format(item.unitPrice)}</small>
              <strong>{money.format(item.unitPrice * item.quantity)}</strong>
            </div>)}
          </section>

          <section className="order-totals">
            <div><span>Товары</span><span>{money.format(order.subtotal)}</span></div>
            <div><span>Доставка</span><span>{order.deliveryFeePending ? "уточняет менеджер" : money.format(order.deliveryFee)}</span></div>
            <div className="total"><span>Итого</span><span>{money.format(order.total)}</span></div>
            {order.paidAmount > 0 && <div><span>Оплачено</span><span>{money.format(order.paidAmount)}</span></div>}
            {order.refundedAmount > 0 && <div><span>Возвращено</span><span>{money.format(order.refundedAmount)}</span></div>}
            {order.amountDue > 0 && <div className="total"><span>К доплате</span><span>{money.format(order.amountDue)}</span></div>}
            {order.hasPreorder && <p className="order-note">В заказе есть товар, наличие которого должен подтвердить менеджер. Оплата откроется после подтверждения.</p>}
            {order.deliveryFeePending && <p className="order-note">{deliveryPendingText(order)}</p>}
          </section>

          <section className="order-facts">
            <div><small>Способ получения</small><span>{deliveryLabels[order.deliveryMethod] ?? order.deliveryMethod}</span></div>
            {order.address && <div><small>Адрес</small><span>{order.address}</span></div>}
            {order.trackNumber && <div><small>Трек-номер СДЭК</small><span>{order.trackNumber}</span></div>}
            <div><small>Оплата</small><span className={order.amountDue === 0 ? "payment-state paid" : "payment-state unpaid"}>{paymentLabels[order.paymentStatus] ?? order.paymentStatus}</span></div>
            {order.paymentReady && order.amountDue > 0 && <button className="primary-button" disabled={paying} onClick={() => void pay()}>
              {paying ? "Открываем оплату…" : `Оплатить ${money.format(order.amountDue)}`}
            </button>}
            {!order.paymentReady && order.amountDue > 0 && <p className="order-note">Заказ принят. Менеджер проверит состав и доставку, после сохранения здесь автоматически появится кнопка оплаты.</p>}
            {order.amountDue === 0 && order.paymentStatus === "paid" && <p className="order-note">Заказ полностью оплачен.</p>}
            <div><small>Получатель</small><span>{order.customerName}, {order.phone}</span></div>
            {order.comment && <div><small>Комментарий</small><span>{order.comment}</span></div>}
          </section>
        </>}
      </div>
    </section>
  </main>;
}
