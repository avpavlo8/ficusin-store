import { useState } from "react";
import { money, statusLabels } from "./adminShared";
import type { AdminData, Section } from "./adminTypes";
import { AdminIcon } from "./AdminWorkspace";
import { AdminRevenue } from "./AdminRevenue";

type Navigate = (section: Section, options?: { orderNumber?: string; wholesaleOnly?: boolean }) => void;

export function Dashboard({ data, onNavigate }: { data: AdminData; onNavigate: Navigate }) {
  const { dashboard, permissions } = data;
  const [filter, setFilter] = useState("all");
  const can = (permission: string) => permissions.includes(permission);
  const recent = dashboard.recentOrders || [];
  const visible = recent.filter(order => filter === "all" || (filter === "active" ? !["cancelled", "completed"].includes(order.status) : order.status === filter));
  const stats: Array<{ label: string; value: number; note: string; section: Section; permission: string; wholesaleOnly?: boolean }> = [
    { label: "Товары", value: dashboard.products, note: `${dashboard.variants} вариантов в каталоге`, section: "products", permission: "products.read" },
    { label: "Заказы", value: dashboard.orders, note: "за всё время", section: "orders", permission: "orders.read" },
    { label: "Клиенты", value: dashboard.customers, note: "розница и опт", section: "customers", permission: "customers.read" },
    { label: "Оптовые заявки", value: dashboard.wholesalePending, note: "ожидают проверки", section: "customers", permission: "customers.read", wholesaleOnly: true },
  ];
  return <>
    <div className="workspace-welcome"><div><p className="eyebrow">{new Date().toLocaleDateString("ru-RU", { weekday: "long", day: "numeric", month: "long" })}</p><h1>Всё <em>растёт.</em></h1><p>{data.user.fullName.split(" ")[0]}, ваш магазин — в одном взгляде.</p></div>
      {can("orders.read") && <button type="button" className="workspace-primary" onClick={() => onNavigate("orders")}>Открыть заказы <AdminIcon name="arrow" /></button>}
    </div>
    <div className="workspace-metrics">{stats.filter(stat => can(stat.permission)).map(stat => <button type="button" key={stat.label} onClick={() => onNavigate(stat.section, { wholesaleOnly: stat.wholesaleOnly })}>
      <span>{stat.label}</span><strong>{stat.value.toLocaleString("ru-RU")}</strong><small>{stat.note}</small><AdminIcon name="arrow" />
    </button>)}</div>
    <div className="workspace-dashboard-grid">
      <div className="workspace-dashboard-main">
        {can("analytics.read") && <AdminRevenue onOpen={() => onNavigate("analytics")} />}
        {can("orders.read") && <section className="workspace-panel workspace-recent">
          <header className="workspace-panel-heading"><div><p className="eyebrow">Продажи</p><h2>Последние заказы</h2></div><button type="button" className="workspace-text-button" onClick={() => onNavigate("orders")}>Все заказы ↗</button></header>
          <div className="workspace-filters" role="group" aria-label="Фильтр последних заказов">{[{ id: "all", label: "Все" }, { id: "active", label: "В работе" }, { id: "completed", label: "Завершённые" }, { id: "cancelled", label: "Отменённые" }].map(item => <button type="button" aria-pressed={filter === item.id} key={item.id} onClick={() => setFilter(item.id)}>{item.label}</button>)}</div>
          <div className="workspace-order-columns" aria-hidden="true"><span>Заказ / клиент</span><span>Сумма</span><span>Статус</span><span /></div>
          <div className="admin-order-list">{visible.map(order => <button type="button" key={order.orderNumber} onClick={() => onNavigate("orders", { orderNumber: order.orderNumber })}>
            <div><strong>{order.orderNumber}</strong><small>{order.customerName}</small></div><span>{money.format(order.total)}</span><b className="workspace-status" data-status={order.status}>{statusLabels[order.status] || order.status}</b><AdminIcon name="arrow" />
          </button>)}</div>
          {!visible.length && <p className="workspace-empty">{recent.length ? "Среди последних заказов нет заказов с этим статусом." : "Первые заказы появятся здесь."}</p>}
          {recent.length > 0 && <p className="workspace-table-note">Последние {recent.length} заказов · полная история в разделе «Заказы»</p>}
        </section>}
        <section className="workspace-panel workspace-shortcuts"><p className="eyebrow">Быстрый переход</p><div>
          {can("products.read") && <button type="button" onClick={() => onNavigate("products")}><AdminIcon name="products" /><strong>Каталог товаров</strong><span>Карточки, цены и остатки</span><AdminIcon name="arrow" /></button>}
          {can("procurement.read") && <button type="button" aria-label="Открыть снабжение" onClick={() => onNavigate("procurement")}><AdminIcon name="procurement" /><strong>Закупки</strong><span>Поставки и маркетплейсы</span><AdminIcon name="arrow" /></button>}
          {can("products.read") && <button type="button" aria-label="Настроить витрину" onClick={() => onNavigate("collections")}><AdminIcon name="collections" /><strong>Подборки</strong><span>Витрина вашего магазина</span><AdminIcon name="arrow" /></button>}
        </div></section>
      </div>
      <aside className="workspace-dashboard-rail" aria-label="Состояние магазина">
        <section className="workspace-attention"><p className="eyebrow">На контроле</p><h2>Важное сейчас</h2>
          {can("customers.read") && <button type="button" onClick={() => onNavigate("customers", { wholesaleOnly: true })}><div><strong>{dashboard.wholesalePending ? `${dashboard.wholesalePending} оптовых заявок` : "Заявки разобраны"}</strong><small>{dashboard.wholesalePending ? "Ожидают вашей проверки" : "Нет заявок, ожидающих проверки"}</small></div><AdminIcon name="arrow" /></button>}
          <div className="admin-alert"><div><strong>{dashboard.lastSync?.status === "success" ? "Каталог синхронизирован" : "Проверьте синхронизацию"}</strong><p>{dashboard.lastSync ? `СБИС · обновлено позиций: ${dashboard.lastSync.itemsUpdated}` : "Нет данных о последней синхронизации СБИС"}</p></div></div>
          {can("procurement.read") && <button type="button" onClick={() => onNavigate("procurement")}><div><strong>Поставки и закупки</strong><small>Открыть рабочую очередь</small></div><AdminIcon name="arrow" /></button>}
        </section>
        <section className="workspace-botanical" aria-label="Живые растения для живых людей"><p className="eyebrow">Фикусин · с заботой</p><h2>Больше зелени.<br /><em>Больше жизни.</em></h2><img src="/assets/redesign/home-hero-4k.webp" alt="" /><span>Живые растения<br />для живых людей ↗</span></section>
        <p className="workspace-rail-note">Всё для вашего магазина.<br />В одном пространстве.</p>
      </aside>
    </div>
  </>;
}
