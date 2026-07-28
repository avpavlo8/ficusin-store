import { useEffect, useState } from "react";

type AdminData = {
  user: { fullName: string };
  dashboard: {
    products: number;
    variants: number;
    orders: number;
    customers: number;
    wholesalePending: number;
    lastSync: null | { status: string; itemsUpdated: number };
    recentOrders: Array<{
      orderNumber: string;
      customerName: string;
      total: number;
      status: string;
    }>;
  };
};

const money = new Intl.NumberFormat("ru-RU", {
  style: "currency",
  currency: "RUB",
  maximumFractionDigits: 0,
});

export default function AdminPage() {
  const [data, setData] = useState<AdminData | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await fetch("/api/v1/admin/dashboard");
      if (response.status === 401) {
        window.location.assign("/login?returnTo=/admin");
        return;
      }
      if (response.status === 403) {
        window.location.assign("/account");
        return;
      }
      const result = (await response.json()) as AdminData & { error?: string };
      if (!response.ok) {
        setError(result.error || "Не удалось загрузить панель");
        return;
      }
      setData(result);
    }
    void load();
  }, []);

  if (!data) {
    return (
      <main className="admin-page">
        <section className="admin-main">
          {error && <p className="auth-error">{error}</p>}
        </section>
      </main>
    );
  }
  const { dashboard, user } = data;
  return (
    <main className="admin-page">
      <aside className="admin-sidebar">
        <a className="admin-logo" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></a>
        <p>Управление магазином</p>
        <nav>
          <a className="active" href="#dashboard"><span>⌂</span>Обзор</a>
          <a href="#catalog"><span>⌁</span>Каталог</a>
          <a href="#orders"><span>□</span>Заказы</a>
          <a href="#customers"><span>○</span>Клиенты</a>
          <a href="#integrations"><span>↻</span>Интеграции</a>
        </nav>
        <a className="admin-store-link" href="/">← Вернуться в магазин</a>
      </aside>
      <section className="admin-main" id="dashboard">
        <header className="admin-topbar">
          <div><p className="eyebrow">Панель управления</p><h1>Добрый день, {user.fullName.split(" ")[0]}</h1></div>
        </header>
        <div className="admin-alert">
          <div>
            <strong>{dashboard.lastSync?.status === "success" ? "Каталог Saby синхронизирован" : "Ожидается синхронизация Saby"}</strong>
            <p>{dashboard.lastSync ? `Обновлено позиций: ${dashboard.lastSync.itemsUpdated}` : "После синхронизации товары, цены и остатки появятся здесь."}</p>
          </div>
        </div>
        <div className="admin-stats">
          <article><span>Товары</span><strong>{dashboard.products}</strong><small>{dashboard.variants} вариантов</small></article>
          <article><span>Заказы</span><strong>{dashboard.orders}</strong><small>за всё время</small></article>
          <article><span>Клиенты</span><strong>{dashboard.customers}</strong><small>розница и опт</small></article>
          <article className={dashboard.wholesalePending ? "attention" : ""}><span>Оптовые заявки</span><strong>{dashboard.wholesalePending}</strong><small>ожидают проверки</small></article>
        </div>
        <div className="admin-columns">
          <section className="admin-block" id="orders">
            <div className="admin-block-heading"><div><p className="eyebrow">Продажи</p><h2>Последние заказы</h2></div></div>
            {dashboard.recentOrders.length ? (
              <div className="admin-order-list">
                {dashboard.recentOrders.map((order) => (
                  <article key={order.orderNumber}>
                    <div><strong>{order.orderNumber}</strong><small>{order.customerName}</small></div>
                    <span>{money.format(order.total)}</span><b>{order.status}</b>
                  </article>
                ))}
              </div>
            ) : <div className="admin-empty"><span>□</span><p>Заказов пока нет</p></div>}
          </section>
          <section className="admin-block" id="integrations">
            <div className="admin-block-heading"><div><p className="eyebrow">Обмен данными</p><h2>Интеграции</h2></div></div>
            <div className="integration-list">
              <article><span className="integration-logo">S</span><div><strong>Saby Retail</strong><small>Каталог, цены и остатки</small></div></article>
              <article><span className="integration-logo">C</span><div><strong>СДЭК</strong><small>Расчёт доставки и ПВЗ</small></div></article>
              <article><span className="integration-logo">₽</span><div><strong>Онлайн-оплата</strong><small>Подключим после проверки сайта</small></div></article>
            </div>
          </section>
        </div>
      </section>
    </main>
  );
}
