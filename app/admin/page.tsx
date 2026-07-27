import Link from "next/link";
import { redirect } from "next/navigation";
import { requireChatGPTUser } from "../chatgpt-auth";

export const dynamic = "force-dynamic";

const OWNER_EMAIL = "avpavlomail@gmail.com";

type DashboardData = {
  products: number;
  variants: number;
  orders: number;
  customers: number;
  wholesalePending: number;
  lastSync: {
    status: string;
    started_at: string;
    items_updated: number;
    errors_count: number;
  } | null;
  recentOrders: Array<{
    order_number: string;
    customer_name: string;
    total: number;
    status: string;
    created_at: string;
  }>;
};

async function loadDashboard(): Promise<DashboardData> {
  const empty: DashboardData = {
    products: 0,
    variants: 0,
    orders: 0,
    customers: 0,
    wholesalePending: 0,
    lastSync: null,
    recentOrders: [],
  };

  try {
    const { env } = await import("cloudflare:workers");
    const [
      products,
      variants,
      orders,
      customers,
      wholesalePending,
      lastSync,
      recentOrders,
    ] = await env.DB.batch([
      env.DB.prepare("SELECT count(*) AS count FROM products"),
      env.DB.prepare("SELECT count(*) AS count FROM product_variants"),
      env.DB.prepare("SELECT count(*) AS count FROM orders"),
      env.DB.prepare("SELECT count(*) AS count FROM customers"),
      env.DB.prepare(
        "SELECT count(*) AS count FROM customers WHERE wholesale_status = 'pending'",
      ),
      env.DB.prepare(
        "SELECT status, started_at, items_updated, errors_count FROM sync_runs WHERE source = 'saby' ORDER BY started_at DESC LIMIT 1",
      ),
      env.DB.prepare(
        "SELECT order_number, customer_name, total, status, created_at FROM orders ORDER BY created_at DESC LIMIT 6",
      ),
    ]);

    const count = (result: D1Result) =>
      Number((result.results?.[0] as { count?: number } | undefined)?.count ?? 0);

    return {
      products: count(products),
      variants: count(variants),
      orders: count(orders),
      customers: count(customers),
      wholesalePending: count(wholesalePending),
      lastSync:
        (lastSync.results?.[0] as DashboardData["lastSync"] | undefined) ?? null,
      recentOrders:
        (recentOrders.results as DashboardData["recentOrders"] | undefined) ?? [],
    };
  } catch {
    return empty;
  }
}

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(value);

export default async function AdminPage() {
  const user = await requireChatGPTUser("/admin");
  if (user.email.toLowerCase() !== OWNER_EMAIL) redirect("/");

  const data = await loadDashboard();
  const initials = user.displayName
    .split(" ")
    .slice(0, 2)
    .map((part) => part.charAt(0))
    .join("")
    .toUpperCase();

  return (
    <main className="admin-page">
      <aside className="admin-sidebar">
        <Link className="admin-logo" href="/">
          <span className="brand-mark">⌇</span>
          <span>Фикусин</span>
        </Link>
        <p>Управление магазином</p>
        <nav aria-label="Разделы администрирования">
          <a className="active" href="#dashboard"><span>⌂</span>Обзор</a>
          <a href="#catalog"><span>⌁</span>Каталог</a>
          <a href="#orders"><span>□</span>Заказы</a>
          <a href="#customers"><span>○</span>Клиенты</a>
          <a href="#discounts"><span>%</span>Скидки</a>
          <a href="#content"><span>✎</span>Контент</a>
          <a href="#integrations"><span>↻</span>Интеграции</a>
        </nav>
        <Link className="admin-store-link" href="/">← Вернуться в магазин</Link>
      </aside>

      <section className="admin-main" id="dashboard">
        <header className="admin-topbar">
          <div>
            <p className="eyebrow">Панель управления</p>
            <h1>Добрый день, Александр</h1>
          </div>
          <div className="admin-user">
            <span>{initials || "АП"}</span>
            <div><strong>{user.displayName}</strong><small>Владелец</small></div>
          </div>
        </header>

        <div className="admin-alert">
          <div><strong>Следующий шаг — подключить Saby</strong><p>После добавления служебного доступа каталог, цены и остатки появятся здесь автоматически.</p></div>
          <span>Не подключено</span>
        </div>

        <div className="admin-stats">
          <article><span>Товары</span><strong>{data.products}</strong><small>{data.variants} вариантов</small></article>
          <article><span>Заказы</span><strong>{data.orders}</strong><small>за всё время</small></article>
          <article><span>Клиенты</span><strong>{data.customers}</strong><small>розница и опт</small></article>
          <article className={data.wholesalePending ? "attention" : ""}><span>Оптовые заявки</span><strong>{data.wholesalePending}</strong><small>ожидают проверки</small></article>
        </div>

        <section className="admin-block" id="catalog">
          <div className="admin-block-heading">
            <div><p className="eyebrow">Каталог</p><h2>Товары и цены</h2></div>
            <button type="button" disabled>Добавить товар</button>
          </div>
          <div className="admin-modules">
            <article><span>01</span><div><h3>Синхронизация с Saby</h3><p>Артикулы, одна базовая цена, варианты и остатки по складам.</p></div><b>Готово к подключению</b></article>
            <article><span>02</span><div><h3>Импорт цен из XLSX</h3><p>Предпросмотр изменений по артикулу без слепой перезаписи.</p></div><b>Спроектировано</b></article>
            <article><span>03</span><div><h3>Фотографии и описания</h3><p>Несколько фото для товара и каждого размера растения.</p></div><b>Хранилище готово</b></article>
          </div>
        </section>

        <div className="admin-columns">
          <section className="admin-block" id="orders">
            <div className="admin-block-heading"><div><p className="eyebrow">Продажи</p><h2>Последние заказы</h2></div></div>
            {data.recentOrders.length ? (
              <div className="admin-order-list">
                {data.recentOrders.map((order) => (
                  <article key={order.order_number}>
                    <div><strong>{order.order_number}</strong><small>{order.customer_name}</small></div>
                    <span>{money(order.total)}</span>
                    <b>{order.status}</b>
                  </article>
                ))}
              </div>
            ) : (
              <div className="admin-empty"><span>□</span><p>Заказов пока нет</p></div>
            )}
          </section>

          <section className="admin-block" id="integrations">
            <div className="admin-block-heading"><div><p className="eyebrow">Обмен данными</p><h2>Интеграции</h2></div></div>
            <div className="integration-list">
              <article><span className="integration-logo">S</span><div><strong>Saby Retail</strong><small>Цены, остатки и заказы</small></div><b>Ожидает доступа</b></article>
              <article><span className="integration-logo">C</span><div><strong>СДЭК</strong><small>Расчёт доставки и ПВЗ</small></div><b>Не подключено</b></article>
              <article><span className="integration-logo">₽</span><div><strong>Онлайн-оплата</strong><small>Провайдер ещё не выбран</small></div><b>Не подключено</b></article>
            </div>
          </section>
        </div>

        <section className="admin-block" id="customers">
          <div className="admin-block-heading"><div><p className="eyebrow">Покупатели</p><h2>Клиенты и скидки</h2></div></div>
          <div className="customer-rules">
            <article><span>Розница</span><h3>Накопительная скидка</h3><p>От 1 штуки. Процент зависит от суммы завершённых покупок.</p></article>
            <article><span>Опт</span><h3>После проверки</h3><p>Индивидуальная скидка и минимальное количество для каждого артикула.</p></article>
            <article id="discounts"><span>Гость</span><h3>Без регистрации</h3><p>Розничная цена без скидки и стандартные способы доставки.</p></article>
          </div>
        </section>

        <section className="admin-block" id="content">
          <div className="admin-block-heading"><div><p className="eyebrow">Редактор</p><h2>Контент магазина</h2></div></div>
          <div className="content-actions">
            <span>Описания растений</span><span>Полезные статьи</span><span>Похожие товары</span><span>Сопутствующие товары</span><span>Баннеры главной</span>
          </div>
        </section>
      </section>
    </main>
  );
}
