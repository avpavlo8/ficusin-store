import Link from "next/link";
import { chatGPTSignOutPath, requireChatGPTUser } from "../chatgpt-auth";

export const dynamic = "force-dynamic";

type OrderRow = {
  order_number: string;
  total: number;
  status: string;
  payment_status: string;
  created_at: string;
};

const statusLabels: Record<string, string> = {
  new: "Новый",
  confirmed: "Подтверждён",
  packing: "Собираем",
  shipped: "Отправлен",
  completed: "Выполнен",
  cancelled: "Отменён",
};

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(value);

async function getOrders(email: string): Promise<OrderRow[]> {
  try {
    const { env } = await import("cloudflare:workers");
    const result = await env.DB.prepare(
      `SELECT order_number, total, status, payment_status, created_at
       FROM orders
       WHERE lower(email) = lower(?)
       ORDER BY created_at DESC
       LIMIT 20`,
    )
      .bind(email)
      .all<OrderRow>();
    return result.results;
  } catch {
    return [];
  }
}

export default async function AccountPage() {
  const user = await requireChatGPTUser("/account");
  const orders = await getOrders(user.email);
  const initial = user.displayName.trim().charAt(0).toUpperCase() || "Ф";

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
          <h1>{user.displayName}</h1>
          <p>{user.email}</p>
          <nav aria-label="Разделы личного кабинета">
            <a className="active" href="#orders">Мои заказы</a>
            <span>Адреса доставки <small>скоро</small></span>
            <span>Избранное <small>скоро</small></span>
            {user.email.toLowerCase() === "avpavlomail@gmail.com" && (
              <Link href="/admin">Управление магазином</Link>
            )}
          </nav>
          <a className="signout-link" href={chatGPTSignOutPath("/")}>Выйти из аккаунта</a>
        </aside>

        <div className="account-content">
          <div className="account-title">
            <div>
              <p className="eyebrow">Личный кабинет</p>
              <h2>Мои заказы</h2>
            </div>
            <span>Временный вход через ChatGPT</span>
          </div>

          <div className="auth-notice">
            <strong>Тестовая авторизация</strong>
            <p>Позже мы заменим этот вход на SMS-код по номеру телефона. История заказов и данные кабинета сохранятся.</p>
          </div>

          <section id="orders" className="orders-list">
            {orders.length ? (
              orders.map((order) => (
                <article key={order.order_number}>
                  <div><small>Заказ</small><strong>{order.order_number}</strong></div>
                  <div><small>Дата</small><span>{new Date(order.created_at).toLocaleDateString("ru-RU")}</span></div>
                  <div><small>Сумма</small><span>{money(order.total)}</span></div>
                  <div><small>Статус</small><span className="order-status">{statusLabels[order.status] ?? order.status}</span></div>
                </article>
              ))
            ) : (
              <div className="orders-empty">
                <span>⌁</span>
                <h3>Заказов пока нет</h3>
                <p>После оформления заказа он появится здесь, если email в заказе совпадает с email аккаунта.</p>
                <Link className="primary-button" href="/#catalog">Перейти в каталог</Link>
              </div>
            )}
          </section>
        </div>
      </section>
    </main>
  );
}
