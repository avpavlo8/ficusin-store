import Link from "next/link";
import { requireStoreUser } from "../../lib/server/auth";
import { getPostgres } from "../../lib/server/postgres";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const wholesaleLabels: Record<string, string> = {
  pending: "Оптовая заявка на проверке",
  approved: "Оптовый аккаунт подтверждён",
  rejected: "Оптовая заявка отклонена",
};

const orderStatusLabels: Record<string, string> = {
  new: "Новый",
  confirmed: "Подтверждён",
  processing: "Собирается",
  shipped: "Передан в доставку",
  completed: "Выполнен",
  cancelled: "Отменён",
};

type AccountOrder = {
  order_number: string;
  delivery_method: string;
  total: number | string;
  status: string;
  created_at: Date | string;
  items_count: number | string;
};

const money = (value: number | string) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(Number(value));

export default async function AccountPage() {
  const user = await requireStoreUser("/account");
  const orders = await getPostgres()<AccountOrder[]>`
    SELECT
      o.order_number,
      o.delivery_method,
      o.total,
      o.status,
      o.created_at,
      COALESCE(SUM(oi.quantity), 0) AS items_count
    FROM orders o
    LEFT JOIN order_items oi ON oi.order_id = o.id
    WHERE o.customer_id = ${user.id}
    GROUP BY o.id
    ORDER BY o.created_at DESC
    LIMIT 50
  `;
  const initial = user.fullName.trim().charAt(0).toUpperCase() || "Ф";
  const discount = user.retailDiscountBps / 100;

  return (
    <main className="account-page">
      <header className="account-header">
        <Link className="brand" href="/">
          <span className="brand-mark">⌇</span>
          <span>Фикусин</span>
        </Link>
        <Link className="account-back" href="/">← Вернуться в магазин</Link>
      </header>

      <section className="account-shell">
        <aside className="account-sidebar">
          <div className="account-avatar">{initial}</div>
          <h1>{user.fullName}</h1>
          <p>{user.phone}<br />{user.email}</p>
          <nav aria-label="Разделы личного кабинета">
            <a className="active" href="#orders">Мои заказы</a>
            <span>Адреса доставки <small>скоро</small></span>
            <span>Избранное <small>скоро</small></span>
          </nav>
          <form action="/api/auth/logout" method="post">
            <button className="signout-link" type="submit">Выйти из аккаунта</button>
          </form>
        </aside>

        <div className="account-content">
          <div className="account-title">
            <div>
              <p className="eyebrow">Личный кабинет</p>
              <h2>Мои заказы</h2>
            </div>
            <span>{user.accountType === "wholesale" ? "Оптовый клиент" : "Розничный клиент"}</span>
          </div>

          {user.accountType === "wholesale" ? (
            <div className="auth-notice">
              <strong>{wholesaleLabels[user.wholesaleStatus] ?? "Статус уточняется"}</strong>
              <p>После проверки реквизитов мы включим оптовые условия, минимальные количества и оплату по счёту.</p>
            </div>
          ) : (
            <div className="auth-notice">
              <strong>Персональная скидка: {discount.toLocaleString("ru-RU")}%</strong>
              <p>Скидка увеличивается автоматически после выполнения оплаченных заказов.</p>
            </div>
          )}

          <section id="orders" className="orders-list">
            {orders.length ? (
              orders.map((order) => (
                <article key={order.order_number}>
                  <div>
                    <small>Заказ</small>
                    <strong>{order.order_number}</strong>
                  </div>
                  <div>
                    <small>Оформлен</small>
                    <span>{new Date(order.created_at).toLocaleDateString("ru-RU")}</span>
                  </div>
                  <div>
                    <small>Состав</small>
                    <span>{Number(order.items_count)} шт.</span>
                  </div>
                  <div>
                    <small>{orderStatusLabels[order.status] ?? order.status}</small>
                    <strong>{money(order.total)}</strong>
                  </div>
                </article>
              ))
            ) : (
              <div className="orders-empty">
                <span>⌁</span>
                <h3>Заказов пока нет</h3>
                <p>Новые заказы, оформленные в этом аккаунте, появятся здесь.</p>
                <Link className="primary-button" href="/#catalog">Перейти в каталог</Link>
              </div>
            )}
          </section>
        </div>
      </section>
    </main>
  );
}
