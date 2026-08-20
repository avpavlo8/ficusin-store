import { Fragment, useEffect, useMemo, useState } from "react";
import { AdminOrderEditor } from "./AdminOrderEditor";
import { Dialog, PageHeading, api, money, orderStatuses, paymentLabels, paymentMethodLabels, roleLabel, roles, statusLabels } from "./adminShared";
import type { AdminData, Customer, Order, Role, Section } from "./adminTypes";

export function Dashboard({ data, onNavigate }: {
  data: AdminData;
  onNavigate: (section: Section, options?: { orderNumber?: string; wholesaleOnly?: boolean }) => void;
}) {
  const { dashboard, user } = data;
  return <>
    <PageHeading eyebrow="Панель управления" title={`Добрый день, ${user.fullName.split(" ")[0]}`} text="Состояние магазина на текущий момент" />
    <div className="admin-alert"><div><strong>{dashboard.lastSync?.status === "success" ? "Каталог Saby синхронизирован" : "Ожидается синхронизация Saby"}</strong><p>{dashboard.lastSync ? `Обновлено позиций: ${dashboard.lastSync.itemsUpdated}` : "Данных о последней синхронизации пока нет."}</p></div></div>
    <div className="admin-stats">
      <button type="button" onClick={() => onNavigate("products")}><span>Товары</span><strong>{dashboard.products}</strong><small>{dashboard.variants} вариантов</small></button>
      <button type="button" onClick={() => onNavigate("orders")}><span>Заказы</span><strong>{dashboard.orders}</strong><small>за всё время</small></button>
      <button type="button" onClick={() => onNavigate("customers")}><span>Клиенты</span><strong>{dashboard.customers}</strong><small>розница и опт</small></button>
      <button type="button" className={dashboard.wholesalePending ? "attention" : ""} onClick={() => onNavigate("customers", { wholesaleOnly: true })}><span>Оптовые заявки</span><strong>{dashboard.wholesalePending}</strong><small>ожидают проверки</small></button>
    </div>
    <section className="admin-block"><div className="admin-block-heading"><div><p className="eyebrow">Продажи</p><h2>Последние заказы</h2></div></div>
      <div className="admin-order-list">{dashboard.recentOrders.map((order) => (
        <button type="button" key={order.orderNumber} onClick={() => onNavigate("orders", { orderNumber: order.orderNumber })}>
          <div><strong>{order.orderNumber}</strong><small>{order.customerName}</small></div>
          <span>{money.format(order.total)}</span>
          <b>{statusLabels[order.status] || order.status}</b>
        </button>
      ))}</div>
    </section>
  </>;
}

export function Customers({ can, wholesaleOnly, onError }: { can: (permission: string) => boolean; wholesaleOnly?: boolean; onError: (value: string) => void }) {
  const [items, setItems] = useState<Customer[]>([]);
  const [query, setQuery] = useState("");
  const [pendingOnly, setPendingOnly] = useState(Boolean(wholesaleOnly));
  const [editing, setEditing] = useState<Customer | null>(null);
  useEffect(() => { api<{ customers: Customer[] }>("/api/v1/admin/customers").then((data) => setItems(data.customers)).catch((error) => onError(error.message)); }, [onError]);
  const filtered = useMemo(() => items.filter((item) => {
    if (pendingOnly && !(item.accountType === "wholesale" && item.wholesaleStatus === "pending")) return false;
    return `${item.fullName} ${item.lastName} ${item.phone} ${item.email}`.toLowerCase().includes(query.toLowerCase());
  }), [items, query, pendingOnly]);
  const openCard = (customer: Customer) => { if (can("customers.edit")) setEditing(customer); };
  return <>
    <PageHeading eyebrow="CRM" title="Клиенты" text="Профили, покупки, адреса, скидки и доступ сотрудников" />
    <div className="admin-toolbar">
      <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Поиск по имени, телефону или email" />
      <label className="admin-checkbox"><input type="checkbox" checked={pendingOnly} onChange={(event) => setPendingOnly(event.target.checked)} />Только оптовые заявки</label>
      <span>{filtered.length} клиентов</span>
    </div>
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Клиент</th><th>Контакты и адрес</th><th>Покупки</th><th>Тип</th><th>Доступ</th><th /></tr></thead><tbody>
      {filtered.map((customer) => <tr
        key={customer.id}
        className={`${!customer.active ? "muted" : ""} ${can("customers.edit") ? "clickable" : ""}`}
        onClick={() => openCard(customer)}
      >
        <td><strong>{[customer.lastName, customer.fullName, customer.patronymic].filter(Boolean).join(" ")}</strong><small>с {new Date(customer.createdAt).toLocaleDateString("ru-RU")}</small></td>
        <td><a href={`tel:${customer.phone}`} onClick={(event) => event.stopPropagation()}>{customer.phone}</a><small>{customer.email || "Email не указан"}</small><small>{customer.deliveryAddress || "Адрес не указан"}</small></td>
        <td><strong>{money.format(customer.lifetimeSpend)}</strong><small>{customer.ordersCount} заказов · скидка {customer.retailDiscountBps / 100}%</small></td>
        <td><span className="admin-pill">{customer.accountType === "wholesale" ? "Опт" : "Розница"}</span><small>{customer.wholesaleStatus}</small></td>
        <td><strong>{roleLabel(customer.adminRole)}</strong><small>{customer.active ? "Активен" : "Заблокирован"}</small></td>
        <td>{can("customers.edit") && <button className="admin-action" onClick={() => setEditing(customer)}>Изменить</button>}</td>
      </tr>)}</tbody></table></div>
    {editing && <CustomerDialog customer={editing} owner={can("roles.edit")} onClose={() => setEditing(null)} onSaved={(customer) => { setItems((current) => current.map((item) => item.id === customer.id ? customer : item)); setEditing(null); }} onError={onError} />}
  </>;
}

export function CustomerDialog({ customer, owner, onClose, onSaved, onError }: { customer: Customer; owner: boolean; onClose: () => void; onSaved: (value: Customer) => void; onError: (value: string) => void }) {
  const [form, setForm] = useState(customer);
  const [orders, setOrders] = useState<Order[]>([]);
  useEffect(() => { api<{ orders: Order[] }>("/api/v1/admin/orders").then((data) => setOrders(data.orders.filter((order) => order.customerId === customer.id))).catch((error) => onError(error.message)); }, [customer.id, onError]);
  const save = async () => {
    try {
      const body: Record<string, unknown> = { fullName: form.fullName, lastName: form.lastName, patronymic: form.patronymic, email: form.email, deliveryAddress: form.deliveryAddress, accountType: form.accountType, wholesaleStatus: form.wholesaleStatus };
      if (owner) Object.assign(body, { retailDiscountBps: form.retailDiscountBps, active: form.active, adminRole: form.adminRole });
      const result = await api<{ customer: Customer }>(`/api/v1/admin/customers/${customer.id}`, { method: "PATCH", body: JSON.stringify(body) });
      onSaved(result.customer);
    } catch (error) { onError((error as Error).message); }
  };
  return <Dialog title="Карточка клиента" onClose={onClose}><div className="admin-form-grid">
    <label>Имя<input value={form.fullName} onChange={(event) => setForm({ ...form, fullName: event.target.value })} /></label>
    <label>Фамилия<input value={form.lastName} onChange={(event) => setForm({ ...form, lastName: event.target.value })} /></label>
    <label>Отчество<input value={form.patronymic} onChange={(event) => setForm({ ...form, patronymic: event.target.value })} /></label>
    <label>Email<input type="email" value={form.email} onChange={(event) => setForm({ ...form, email: event.target.value })} /></label>
    <label className="wide">Адрес<input value={form.deliveryAddress} onChange={(event) => setForm({ ...form, deliveryAddress: event.target.value })} /></label>
    <label>Тип<select value={form.accountType} onChange={(event) => setForm({ ...form, accountType: event.target.value })}><option value="retail">Розница</option><option value="wholesale">Опт</option></select></label>
    <label>Статус опта<select value={form.wholesaleStatus} onChange={(event) => setForm({ ...form, wholesaleStatus: event.target.value })}><option value="not_requested">Не запрашивал</option><option value="pending">На проверке</option><option value="approved">Одобрен</option><option value="rejected">Отклонён</option></select></label>
    {owner && <><label>Скидка, %<input type="number" min="0" max="100" step="0.01" value={form.retailDiscountBps / 100} onChange={(event) => setForm({ ...form, retailDiscountBps: Math.round(Number(event.target.value) * 100) })} /></label>
    {form.adminRole === "owner" ? <label>Роль в панели управления<input value="Владелец — назначен через секрет" disabled /></label> : <label>Роль в панели управления<select value={form.adminRole} onChange={(event) => setForm({ ...form, adminRole: event.target.value as Role })}>{roles.map((role) => <option key={role.value} value={role.value}>{role.label}</option>)}</select></label>}
    <label className="admin-checkbox"><input type="checkbox" checked={form.active} onChange={(event) => setForm({ ...form, active: event.target.checked })} />Аккаунт активен</label></>}
  </div><section className="customer-orders"><h3>Заказы клиента</h3>{orders.length === 0 ? <p>Заказов пока нет.</p> : orders.slice(0, 10).map((order) => <article key={order.id}><div><strong>{order.orderNumber}</strong><small>{new Date(order.createdAt).toLocaleDateString("ru-RU")}</small></div><span>{money.format(order.total)}</span><b>{statusLabels[order.status] || order.status}</b></article>)}</section><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" onClick={save}>Сохранить</button></div></Dialog>;
}

export function Orders({ focusOrder, onError }: { focusOrder?: string; onError: (value: string) => void }) {
  const [items, setItems] = useState<Order[]>([]);
  const [opened, setOpened] = useState<number | null>(null);
  useEffect(() => {
    api<{ orders: Order[] }>("/api/v1/admin/orders").then((data) => {
      setItems(data.orders);
      if (focusOrder) {
        const match = data.orders.find((order) => order.orderNumber === focusOrder);
        if (match) setOpened(match.id);
      }
    }).catch((error) => onError(error.message));
  }, [onError, focusOrder]);
  const updateStatus = async (order: Order, status: string) => {
    try { const result = await api<{ order: Order }>(`/api/v1/admin/orders/${order.id}`, { method: "PATCH", body: JSON.stringify({ status, paymentStatus: "" }) }); setItems((current) => current.map((item) => item.id === order.id ? result.order : item)); }
    catch (error) { onError((error as Error).message); }
  };
  const updateOrder = (value: Order) => setItems((current) => current.map((item) => item.id === value.id ? value : item));
  return <><PageHeading eyebrow="Продажи" title="Заказы" text="Состав заказа, контакты, доставка, оплата и текущий статус" />
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Заказ</th><th>Клиент</th><th>Получение</th><th>Сумма</th><th>Статус</th><th /></tr></thead><tbody>{items.map((order) => <Fragment key={order.id}>
      <tr className="clickable" onClick={() => setOpened(opened === order.id ? null : order.id)}><td><strong>{order.orderNumber}</strong><small>{new Date(order.createdAt).toLocaleString("ru-RU")}</small></td><td><strong>{order.customerName}</strong><a href={`tel:${order.phone}`} onClick={(event) => event.stopPropagation()}>{order.phone}</a><small>{order.email}</small></td><td><strong>{order.deliveryMethod}</strong><small>{order.address}</small>{order.trackNumber && <small>Трек: {order.trackNumber}</small>}{order.hasPreorder && <small className="admin-flag">Есть позиции под заказ</small>}{order.deliveryFeePending && <small className="admin-flag">{order.repackRequested ? "Просят одну коробку — посчитайте доставку" : "Доставку нужно посчитать вручную"}</small>}</td><td><strong>{money.format(order.total)}</strong><small className={order.paymentStatus === "paid" ? "payment-state paid" : order.paymentStatus === "pending" || order.paymentStatus === "partially_paid" ? "payment-state unpaid" : "payment-state"}>{paymentLabels[order.paymentStatus] ?? order.paymentStatus}</small>{order.paymentMethod && order.paymentMethod !== "online" && <small className="admin-flag">{paymentMethodLabels[order.paymentMethod]}</small>}</td><td onClick={(event) => event.stopPropagation()}><select value={order.status} onChange={(event) => updateStatus(order, event.target.value)}>{orderStatuses.map((status) => <option value={status} key={status}>{statusLabels[status]}</option>)}</select></td><td><span className="admin-row-arrow" aria-hidden="true">{opened === order.id ? "−" : "→"}</span></td></tr>
      {opened === order.id && <tr className="order-details" key={`${order.id}-details`}><td colSpan={6}><AdminOrderEditor order={order} onSaved={updateOrder} onError={onError} /><div><strong>Комментарий</strong><p>{order.comment || "Нет комментария"}</p></div></td></tr>}
    </Fragment>)}</tbody></table></div></>;
}
