import { Fragment, useCallback, useEffect, useMemo, useState } from "react";

type Role = "owner" | "manager" | "";
type Section = "dashboard" | "products" | "categories" | "orders" | "customers";
type Category = { id: number; parentId: number | null; name: string; slug: string; sortOrder: number; productsCount: number; childrenCount: number };

type AdminData = {
  user: { fullName: string };
  role: Role;
  permissions: string[];
  dashboard: {
    products: number; variants: number; orders: number; customers: number;
    wholesalePending: number;
    lastSync: null | { status: string; itemsUpdated: number };
    recentOrders: Array<{ orderNumber: string; customerName: string; total: number; status: string }>;
  };
};

type Customer = {
  id: number; email: string; phone: string; fullName: string; lastName: string;
  patronymic: string; deliveryAddress: string; accountType: string;
  wholesaleStatus: string; retailDiscountBps: number; lifetimeSpend: number;
  active: boolean; adminRole: Role; ordersCount: number; createdAt: string;
};

type Order = {
  id: number; orderNumber: string; customerId?: number; customerName: string;
  phone: string; email: string; address: string; comment: string;
  deliveryMethod: string; paymentStatus: string; status: string; total: number;
  createdAt: string; items: Array<{ productId: string; productName: string; unitPrice: number; quantity: number }>;
};

type Product = {
  id: number; sabyId: string; slug: string; name: string; latinName: string;
  shortDescription: string; description: string; careInstructions: string;
  status: string; featured: boolean; image: string; price: number; stock: number;
  sku: string; variantLabel: string; heightCm?: number; potDiameterCm?: number;
  packageLengthCm?: number; packageWidthCm?: number; packageHeightCm?: number;
  packageWeightGrams?: number; wholesaleMinQty: number; overrideFields: string[];
  catalogSection: string; plantKind?: string; lightLevel?: string; watering?: string;
  heightClass?: string; careLevel?: string; placement?: string; petSafety?: string;
  growthHabit?: string; sabyUpdatedAt?: string;
  categoryId?: number;
};

const money = new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 });
const roles: Array<{ value: Role; label: string }> = [
  { value: "", label: "Без доступа" }, { value: "manager", label: "Менеджер" },
];
const roleLabel = (role: Role) => role === "owner" ? "Владелец" : roles.find((item) => item.value === role)?.label || "Клиент";
const orderStatuses = ["new", "confirmed", "assembling", "ready", "shipped", "completed", "cancelled"];
const statusLabels: Record<string, string> = {
  new: "Новый", confirmed: "Подтверждён", assembling: "Собирается", ready: "Готов",
  shipped: "Отправлен", completed: "Завершён", cancelled: "Отменён",
  draft: "Черновик", published: "Опубликован", archived: "В архиве",
};

const catalogOptions = {
  catalogSection: [["plants", "Растения"], ["soil", "Грунт"], ["fertilizer", "Удобрения"], ["pots", "Кашпо и горшки"], ["accessories", "Аксессуары"]],
  plantKind: [["aglaonema", "Аглаонема"], ["alocasia", "Алоказия"], ["pineapple", "Ананас"], ["bonsai", "Бонсай"]],
  lightLevel: [["sunny", "Солнечная сторона"], ["diffused", "Яркий рассеянный свет"], ["low_light", "Затемнённое место"]],
  watering: [["frequent", "Частый"], ["moderate", "Умеренный"], ["rare", "Редкий"]],
  heightClass: [["low", "Низкий"], ["medium", "Средний"], ["high", "Высокий"]],
  careLevel: [["easy", "Почти не требует ухода"], ["medium", "Обычный уход"], ["demanding", "Капризный"]],
  placement: [["bathroom", "Ванная"], ["bedroom", "Спальня"], ["office", "Офис"], ["nursery", "Детская"]],
  petSafety: [["safe", "Безопасно для питомцев"], ["caution", "Требует осторожности"]],
  growthHabit: [["compact", "Компактный"], ["upright", "Прямостоячий"], ["trailing", "Ампельный"], ["climbing", "Вьющийся"]],
} satisfies Record<string, string[][]>;

async function api<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: "same-origin", cache: "no-store", ...options,
    headers: { "Content-Type": "application/json", ...(options?.headers || {}) },
  });
  if (response.status === 401) { window.location.assign("/login?returnTo=/admin"); throw new Error("Требуется вход"); }
  if (response.status === 403) throw new Error("Недостаточно прав для этого действия");
  const result = await response.json() as T & { error?: string };
  if (!response.ok) throw new Error(result.error || "Не удалось выполнить операцию");
  return result;
}

export default function AdminPage() {
  const [data, setData] = useState<AdminData | null>(null);
  const [section, setSection] = useState<Section>("dashboard");
  const [error, setError] = useState("");

  useEffect(() => { api<AdminData>("/api/v1/admin/dashboard").then(setData).catch(setError); }, []);
  if (!data) return <main className="admin-page"><section className="admin-main"><p>{error || "Загружаем панель…"}</p></section></main>;

  const can = (permission: string) => data.permissions.includes(permission);
  return (
    <main className="admin-page">
      <aside className="admin-sidebar">
        <a className="admin-logo" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></a>
        <p>Управление магазином</p>
        <nav>
          <Nav active={section === "dashboard"} onClick={() => setSection("dashboard")} icon="⌂">Обзор</Nav>
          {can("products.read") && <Nav active={section === "products"} onClick={() => setSection("products")} icon="⌁">Товары</Nav>}
          {can("products.read") && <Nav active={section === "categories"} onClick={() => setSection("categories")} icon="⌘">Категории</Nav>}
          {can("orders.read") && <Nav active={section === "orders"} onClick={() => setSection("orders")} icon="□">Заказы</Nav>}
          {can("customers.read") && <Nav active={section === "customers"} onClick={() => setSection("customers")} icon="○">Клиенты</Nav>}
        </nav>
        <div className="admin-role"><small>Ваша роль</small><strong>{roleLabel(data.role)}</strong></div>
        <a className="admin-store-link" href="/">← Вернуться в магазин</a>
      </aside>
      <section className="admin-main">
        {error && <div className="admin-message error">{error}<button onClick={() => setError("")}>×</button></div>}
        {section === "dashboard" && <Dashboard data={data} />}
        {section === "customers" && <Customers can={can} onError={setError} />}
        {section === "orders" && <Orders onError={setError} />}
        {section === "products" && <Products can={can} onError={setError} />}
        {section === "categories" && <Categories canEdit={can("products.edit")} onError={setError} />}
      </section>
    </main>
  );
}

function Nav({ active, onClick, icon, children }: { active: boolean; onClick: () => void; icon: string; children: React.ReactNode }) {
  return <button className={active ? "active" : ""} onClick={onClick}><span>{icon}</span>{children}</button>;
}

function Categories({ canEdit, onError }: { canEdit: boolean; onError: (value: string) => void }) {
  const [items, setItems] = useState<Category[]>([]);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [parentId, setParentId] = useState("");
  const load = useCallback(() => api<{ categories: Category[] }>("/api/v1/admin/categories").then((data) => setItems(data.categories)).catch((error) => onError(error.message)), [onError]);
  useEffect(() => { void load(); }, [load]);
  const depth = (item: Category) => {
    let value = 0;
    let parent = item.parentId;
    while (parent && value < 3) {
      value += 1;
      parent = items.find((candidate) => candidate.id === parent)?.parentId ?? null;
    }
    return value;
  };
  const orderedItems = (() => {
    const result: Category[] = [];
    const append = (parentId: number | null) => {
      items
        .filter((item) => item.parentId === parentId)
        .sort((left, right) => left.sortOrder - right.sortOrder || left.name.localeCompare(right.name, "ru"))
        .forEach((item) => { result.push(item); append(item.id); });
    };
    append(null);
    return result;
  })();
  const create = async () => {
    try {
      await api("/api/v1/admin/categories", { method: "POST", body: JSON.stringify({ name, slug, parentId: parentId ? Number(parentId) : null, sortOrder: items.length * 10 }) });
      setName(""); setSlug(""); setParentId(""); load();
    } catch (error) { onError((error as Error).message); }
  };
  const rename = async (item: Category) => {
    const next = window.prompt("Новое название категории", item.name);
    if (!next || next === item.name) return;
    try { await api(`/api/v1/admin/categories/${item.id}`, { method: "PATCH", body: JSON.stringify({ name: next }) }); load(); }
    catch (error) { onError((error as Error).message); }
  };
  const remove = async (item: Category) => {
    if (!window.confirm(`Удалить категорию «${item.name}»?`)) return;
    try { await api(`/api/v1/admin/categories/${item.id}`, { method: "DELETE" }); load(); }
    catch (error) { onError((error as Error).message); }
  };
  return <><PageHeading eyebrow="Структура каталога" title="Категории" text="Три уровня: раздел, группа и вид растения. Категории с товарами защищены от удаления." />
    {canEdit && <div className="admin-toolbar category-create"><input value={name} onChange={(event) => setName(event.target.value)} placeholder="Название" /><input value={slug} onChange={(event) => setSlug(event.target.value.toLowerCase().replace(/[^a-z0-9-]+/g, "-"))} placeholder="slug" /><select value={parentId} onChange={(event) => setParentId(event.target.value)}><option value="">Корневая категория</option>{orderedItems.filter((item) => depth(item) < 2).map((item) => <option value={item.id} key={item.id}>{`${"— ".repeat(depth(item))}${item.name}`}</option>)}</select><button className="admin-primary" disabled={!name.trim() || !slug.trim()} onClick={create}>Добавить</button></div>}
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Категория</th><th>Slug</th><th>Товары</th><th /></tr></thead><tbody>{orderedItems.map((item) => <tr key={item.id}><td><strong style={{ paddingLeft: depth(item) * 24 }}>{depth(item) > 0 ? "↳ " : ""}{item.name}</strong></td><td><code>{item.slug}</code></td><td>{item.productsCount}</td><td>{canEdit && <><button className="admin-action" onClick={() => rename(item)}>Переименовать</button><button className="text-button danger" onClick={() => remove(item)}>Удалить</button></>}</td></tr>)}</tbody></table></div>
  </>;
}


function PageHeading({ eyebrow, title, text }: { eyebrow: string; title: string; text: string }) {
  return <header className="admin-topbar"><div><p className="eyebrow">{eyebrow}</p><h1>{title}</h1><p>{text}</p></div></header>;
}

function Dashboard({ data }: { data: AdminData }) {
  const { dashboard, user } = data;
  return <>
    <PageHeading eyebrow="Панель управления" title={`Добрый день, ${user.fullName.split(" ")[0]}`} text="Состояние магазина на текущий момент" />
    <div className="admin-alert"><div><strong>{dashboard.lastSync?.status === "success" ? "Каталог Saby синхронизирован" : "Ожидается синхронизация Saby"}</strong><p>{dashboard.lastSync ? `Обновлено позиций: ${dashboard.lastSync.itemsUpdated}` : "Данных о последней синхронизации пока нет."}</p></div></div>
    <div className="admin-stats">
      <article><span>Товары</span><strong>{dashboard.products}</strong><small>{dashboard.variants} вариантов</small></article>
      <article><span>Заказы</span><strong>{dashboard.orders}</strong><small>за всё время</small></article>
      <article><span>Клиенты</span><strong>{dashboard.customers}</strong><small>розница и опт</small></article>
      <article className={dashboard.wholesalePending ? "attention" : ""}><span>Оптовые заявки</span><strong>{dashboard.wholesalePending}</strong><small>ожидают проверки</small></article>
    </div>
    <section className="admin-block"><div className="admin-block-heading"><div><p className="eyebrow">Продажи</p><h2>Последние заказы</h2></div></div>
      <div className="admin-order-list">{dashboard.recentOrders.map((order) => <article key={order.orderNumber}><div><strong>{order.orderNumber}</strong><small>{order.customerName}</small></div><span>{money.format(order.total)}</span><b>{statusLabels[order.status] || order.status}</b></article>)}</div>
    </section>
  </>;
}

function Customers({ can, onError }: { can: (permission: string) => boolean; onError: (value: string) => void }) {
  const [items, setItems] = useState<Customer[]>([]);
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<Customer | null>(null);
  useEffect(() => { api<{ customers: Customer[] }>("/api/v1/admin/customers").then((data) => setItems(data.customers)).catch((error) => onError(error.message)); }, [onError]);
  const filtered = useMemo(() => items.filter((item) => `${item.fullName} ${item.lastName} ${item.phone} ${item.email}`.toLowerCase().includes(query.toLowerCase())), [items, query]);
  return <>
    <PageHeading eyebrow="CRM" title="Клиенты" text="Профили, покупки, адреса, скидки и доступ сотрудников" />
    <div className="admin-toolbar"><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Поиск по имени, телефону или email" /><span>{filtered.length} клиентов</span></div>
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Клиент</th><th>Контакты и адрес</th><th>Покупки</th><th>Тип</th><th>Доступ</th><th /></tr></thead><tbody>
      {filtered.map((customer) => <tr key={customer.id} className={!customer.active ? "muted" : ""}>
        <td><strong>{[customer.lastName, customer.fullName, customer.patronymic].filter(Boolean).join(" ")}</strong><small>с {new Date(customer.createdAt).toLocaleDateString("ru-RU")}</small></td>
        <td><a href={`tel:${customer.phone}`}>{customer.phone}</a><small>{customer.email || "Email не указан"}</small><small>{customer.deliveryAddress || "Адрес не указан"}</small></td>
        <td><strong>{money.format(customer.lifetimeSpend)}</strong><small>{customer.ordersCount} заказов · скидка {customer.retailDiscountBps / 100}%</small></td>
        <td><span className="admin-pill">{customer.accountType === "wholesale" ? "Опт" : "Розница"}</span><small>{customer.wholesaleStatus}</small></td>
        <td><strong>{roleLabel(customer.adminRole)}</strong><small>{customer.active ? "Активен" : "Заблокирован"}</small></td>
        <td>{can("customers.edit") && <button className="admin-action" onClick={() => setEditing(customer)}>Изменить</button>}</td>
      </tr>)}</tbody></table></div>
    {editing && <CustomerDialog customer={editing} owner={can("roles.edit")} onClose={() => setEditing(null)} onSaved={(customer) => { setItems((current) => current.map((item) => item.id === customer.id ? customer : item)); setEditing(null); }} onError={onError} />}
  </>;
}

function CustomerDialog({ customer, owner, onClose, onSaved, onError }: { customer: Customer; owner: boolean; onClose: () => void; onSaved: (value: Customer) => void; onError: (value: string) => void }) {
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
    {form.adminRole === "owner" ? <label>Роль в админке<input value="Владелец — назначен через секрет" disabled /></label> : <label>Роль в админке<select value={form.adminRole} onChange={(event) => setForm({ ...form, adminRole: event.target.value as Role })}>{roles.map((role) => <option key={role.value} value={role.value}>{role.label}</option>)}</select></label>}
    <label className="admin-checkbox"><input type="checkbox" checked={form.active} onChange={(event) => setForm({ ...form, active: event.target.checked })} />Аккаунт активен</label></>}
  </div><section className="customer-orders"><h3>Заказы клиента</h3>{orders.length === 0 ? <p>Заказов пока нет.</p> : orders.slice(0, 10).map((order) => <article key={order.id}><div><strong>{order.orderNumber}</strong><small>{new Date(order.createdAt).toLocaleDateString("ru-RU")}</small></div><span>{money.format(order.total)}</span><b>{statusLabels[order.status] || order.status}</b></article>)}</section><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" onClick={save}>Сохранить</button></div></Dialog>;
}

function Orders({ onError }: { onError: (value: string) => void }) {
  const [items, setItems] = useState<Order[]>([]);
  const [opened, setOpened] = useState<number | null>(null);
  useEffect(() => { api<{ orders: Order[] }>("/api/v1/admin/orders").then((data) => setItems(data.orders)).catch((error) => onError(error.message)); }, [onError]);
  const updateStatus = async (order: Order, status: string) => {
    try { const result = await api<{ order: Order }>(`/api/v1/admin/orders/${order.id}`, { method: "PATCH", body: JSON.stringify({ status, paymentStatus: "" }) }); setItems((current) => current.map((item) => item.id === order.id ? result.order : item)); }
    catch (error) { onError((error as Error).message); }
  };
  return <><PageHeading eyebrow="Продажи" title="Заказы" text="Состав заказа, контакты, доставка, оплата и текущий статус" />
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Заказ</th><th>Клиент</th><th>Получение</th><th>Сумма</th><th>Статус</th><th /></tr></thead><tbody>{items.map((order) => <Fragment key={order.id}>
      <tr><td><strong>{order.orderNumber}</strong><small>{new Date(order.createdAt).toLocaleString("ru-RU")}</small></td><td><strong>{order.customerName}</strong><a href={`tel:${order.phone}`}>{order.phone}</a><small>{order.email}</small></td><td><strong>{order.deliveryMethod}</strong><small>{order.address}</small></td><td><strong>{money.format(order.total)}</strong><small>{order.paymentStatus}</small></td><td><select value={order.status} onChange={(event) => updateStatus(order, event.target.value)}>{orderStatuses.map((status) => <option value={status} key={status}>{statusLabels[status]}</option>)}</select></td><td><button className="admin-action" onClick={() => setOpened(opened === order.id ? null : order.id)}>{opened === order.id ? "Скрыть" : "Состав"}</button></td></tr>
      {opened === order.id && <tr className="order-details" key={`${order.id}-details`}><td colSpan={6}><div><strong>Товары</strong>{order.items.map((item) => <p key={`${item.productId}-${item.productName}`}>{item.productName} × {item.quantity} <span>{money.format(item.unitPrice * item.quantity)}</span></p>)}</div><div><strong>Комментарий</strong><p>{order.comment || "Нет комментария"}</p></div></td></tr>}
    </Fragment>)}</tbody></table></div></>;
}

function Products({ can, onError }: { can: (permission: string) => boolean; onError: (value: string) => void }) {
  const [items, setItems] = useState<Product[]>([]);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<number[]>([]);
  const [editing, setEditing] = useState<Product | null>(null);
  const [syncing, setSyncing] = useState<number[] | null>(null);
  useEffect(() => { api<{ products: Product[] }>("/api/v1/admin/products").then((data) => setItems(data.products)).catch((error) => onError(error.message)); }, [onError]);
  const filtered = useMemo(() => items.filter((item) => `${item.name} ${item.sku}`.toLowerCase().includes(query.toLowerCase())), [items, query]);
  const replace = (product: Product) => setItems((current) => current.map((item) => item.id === product.id ? product : item));
  return <><PageHeading eyebrow="Каталог" title="Товары" text="Контент сайта, цены, упаковка, публикация и выборочная синхронизация со СБИС" />
    <div className="admin-toolbar"><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Название или артикул" /><span>{selected.length ? `Выбрано: ${selected.length}` : `${filtered.length} товаров`}</span>{selected.length > 0 && can("products.sync") && <button className="admin-primary" onClick={() => setSyncing(selected)}>Синхронизировать выбранные</button>}</div>
    <div className="admin-table-wrap"><table className="admin-table products"><thead><tr><th><input type="checkbox" checked={filtered.length > 0 && filtered.every((item) => selected.includes(item.id))} onChange={(event) => setSelected(event.target.checked ? filtered.map((item) => item.id) : [])} /></th><th>Товар</th><th>Цена / остаток</th><th>Публикация</th><th>СБИС</th><th /></tr></thead><tbody>{filtered.map((product) => <tr key={product.id}>
      <td><input type="checkbox" checked={selected.includes(product.id)} onChange={(event) => setSelected((current) => event.target.checked ? [...current, product.id] : current.filter((id) => id !== product.id))} /></td>
      <td><div className="admin-product"><img src={product.image || "/assets/hero-monstera.png"} alt="" /><div><strong>{product.name}</strong><small>{product.sku} · {product.variantLabel}</small><a href={`/product/${product.slug}`} target="_blank">Открыть карточку ↗</a></div></div></td>
      <td><strong>{money.format(product.price)}</strong><small>В наличии: {product.stock}</small><small>Опт от {product.wholesaleMinQty} шт.</small></td>
      <td><span className={`admin-pill ${product.status}`}>{statusLabels[product.status] || product.status}</span>{product.overrideFields.length > 0 && <small>Изменено вручную: {product.overrideFields.join(", ")}</small>}</td>
      <td><strong>{product.sabyId ? "Связан" : "Нет связи"}</strong><small>{product.sabyUpdatedAt ? new Date(product.sabyUpdatedAt).toLocaleString("ru-RU") : "Не синхронизировался"}</small>{can("products.sync") && product.sabyId && <button className="text-button" onClick={() => setSyncing([product.id])}>Синхронизировать</button>}</td>
      <td>{can("products.edit") && <button className="admin-action" onClick={() => setEditing(product)}>Изменить</button>}</td>
    </tr>)}</tbody></table></div>
    {editing && <ProductDialog product={editing} onClose={() => setEditing(null)} onSaved={(product) => { replace(product); setEditing(null); }} onError={onError} />}
    {syncing && <SyncDialog count={syncing.length} onClose={() => setSyncing(null)} onSync={async (fields) => { try { await api("/api/v1/admin/products/sync", { method: "POST", body: JSON.stringify({ productIds: syncing, fields }) }); const data = await api<{ products: Product[] }>("/api/v1/admin/products"); setItems(data.products); setSelected([]); setSyncing(null); } catch (error) { onError((error as Error).message); } }} />}
  </>;
}

function ProductDialog({ product, onClose, onSaved, onError }: { product: Product; onClose: () => void; onSaved: (value: Product) => void; onError: (value: string) => void }) {
  const [form, setForm] = useState(product);
  const [categories, setCategories] = useState<Category[]>([]);
  useEffect(() => { api<{ categories: Category[] }>("/api/v1/admin/categories").then((data) => setCategories(data.categories)).catch((error) => onError(error.message)); }, [onError]);
  const number = (value?: number) => Number.isFinite(value) ? value : "";
  const save = async () => {
    try {
      const result = await api<{ product: Product }>(`/api/v1/admin/products/${product.id}`, { method: "PATCH", body: JSON.stringify({
        name: form.name, latinName: form.latinName, shortDescription: form.shortDescription,
        description: form.description, careInstructions: form.careInstructions, status: form.status,
        featured: form.featured, image: form.image, priceMinor: Math.round(form.price * 100),
        variantLabel: form.variantLabel, heightCm: form.heightCm, potDiameterCm: form.potDiameterCm,
        packageLengthCm: form.packageLengthCm, packageWidthCm: form.packageWidthCm,
        packageHeightCm: form.packageHeightCm, packageWeightGrams: form.packageWeightGrams,
        wholesaleMinQty: form.wholesaleMinQty, catalogSection: form.catalogSection, categoryId: form.categoryId,
        plantKind: form.plantKind || "", lightLevel: form.lightLevel || "", watering: form.watering || "",
        heightClass: form.heightClass || "", careLevel: form.careLevel || "", placement: form.placement || "",
        petSafety: form.petSafety || "", growthHabit: form.growthHabit || "",
      }) }); onSaved(result.product);
    } catch (error) { onError((error as Error).message); }
  };
  const setNumeric = (key: keyof Product, value: string) => setForm({ ...form, [key]: value === "" ? undefined : Number(value) });
  return <Dialog title="Редактирование товара" onClose={onClose}><div className="admin-form-grid product-form">
    <label className="wide">Название<input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></label>
    <label>Латинское название<input value={form.latinName} onChange={(event) => setForm({ ...form, latinName: event.target.value })} /></label>
    <label>Название размера<input value={form.variantLabel} onChange={(event) => setForm({ ...form, variantLabel: event.target.value })} /></label>
    <label className="wide">Короткое описание<textarea rows={2} value={form.shortDescription} onChange={(event) => setForm({ ...form, shortDescription: event.target.value })} /></label>
    <label className="wide">Описание<textarea rows={5} value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></label>
    <label className="wide">Уход<textarea rows={4} value={form.careInstructions} onChange={(event) => setForm({ ...form, careInstructions: event.target.value })} /></label>
    <label className="wide">URL фотографии<input value={form.image} onChange={(event) => setForm({ ...form, image: event.target.value })} /></label>
    <label className="wide">Категория<select value={form.categoryId || ""} onChange={(event) => setForm({ ...form, categoryId: event.target.value ? Number(event.target.value) : undefined })}><option value="">Не указано</option>{categories.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
    <label>Освещённость<select value={form.lightLevel || ""} onChange={(event) => setForm({ ...form, lightLevel: event.target.value })}><option value="">Не указано</option>{catalogOptions.lightLevel.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label>Полив<select value={form.watering || ""} onChange={(event) => setForm({ ...form, watering: event.target.value })}><option value="">Не указано</option>{catalogOptions.watering.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label>Высота<select value={form.heightClass || ""} onChange={(event) => setForm({ ...form, heightClass: event.target.value })}><option value="">Не указано</option>{catalogOptions.heightClass.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label>Сложность ухода<select value={form.careLevel || ""} onChange={(event) => setForm({ ...form, careLevel: event.target.value })}><option value="">Не указано</option>{catalogOptions.careLevel.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label>Подходит для<select value={form.placement || ""} onChange={(event) => setForm({ ...form, placement: event.target.value })}><option value="">Не указано</option>{catalogOptions.placement.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label>Для питомцев<select value={form.petSafety || ""} onChange={(event) => setForm({ ...form, petSafety: event.target.value })}><option value="">Не указано</option>{catalogOptions.petSafety.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label>Форма роста<select value={form.growthHabit || ""} onChange={(event) => setForm({ ...form, growthHabit: event.target.value })}><option value="">Не указано</option>{catalogOptions.growthHabit.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label>Цена, ₽<input type="number" min="0" value={form.price} onChange={(event) => setForm({ ...form, price: Number(event.target.value) })} /></label>
    <label>Оптовый минимум<input type="number" min="1" value={form.wholesaleMinQty} onChange={(event) => setForm({ ...form, wholesaleMinQty: Number(event.target.value) })} /></label>
    <label>Высота растения, см<input type="number" value={number(form.heightCm)} onChange={(event) => setNumeric("heightCm", event.target.value)} /></label>
    <label>Диаметр горшка, см<input type="number" value={number(form.potDiameterCm)} onChange={(event) => setNumeric("potDiameterCm", event.target.value)} /></label>
    <label>Упаковка: длина, см<input type="number" value={number(form.packageLengthCm)} onChange={(event) => setNumeric("packageLengthCm", event.target.value)} /></label>
    <label>Ширина, см<input type="number" value={number(form.packageWidthCm)} onChange={(event) => setNumeric("packageWidthCm", event.target.value)} /></label>
    <label>Высота, см<input type="number" value={number(form.packageHeightCm)} onChange={(event) => setNumeric("packageHeightCm", event.target.value)} /></label>
    <label>Вес, г<input type="number" value={number(form.packageWeightGrams)} onChange={(event) => setNumeric("packageWeightGrams", event.target.value)} /></label>
    <label>Статус<select value={form.status} onChange={(event) => setForm({ ...form, status: event.target.value })}><option value="draft">Черновик</option><option value="published">Опубликован</option><option value="archived">Архив</option></select></label>
    <label className="admin-checkbox"><input type="checkbox" checked={form.featured} onChange={(event) => setForm({ ...form, featured: event.target.checked })} />Показывать среди избранных</label>
  </div><p className="admin-hint">Ручные изменения защищены от фоновой синхронизации. Вернуть поле к данным СБИС можно кнопкой «Синхронизировать».</p><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" onClick={save}>Сохранить</button></div></Dialog>;
}

function SyncDialog({ count, onClose, onSync }: { count: number; onClose: () => void; onSync: (fields: string[]) => void }) {
  const options = [{ id: "name", label: "Название" }, { id: "photo", label: "Фото" }, { id: "price", label: "Цена" }, { id: "description", label: "Описание" }, { id: "dimensions", label: "Размеры упаковки", disabled: true }];
  const [fields, setFields] = useState(["price"]);
  return <Dialog title={`Синхронизация: ${count} ${count === 1 ? "товар" : "товаров"}`} onClose={onClose}><p>Выбранные поля будут заменены последними данными, полученными из СБИС.</p><div className="sync-options">{options.map((option) => <label className={option.disabled ? "disabled" : ""} key={option.id}><input type="checkbox" disabled={option.disabled} checked={fields.includes(option.id)} onChange={(event) => setFields((current) => event.target.checked ? [...current, option.id] : current.filter((field) => field !== option.id))} /><span><strong>{option.label}</strong>{option.disabled && <small>СБИС пока не отдаёт эти поля в текущем API</small>}</span></label>)}</div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={fields.length === 0} onClick={() => onSync(fields)}>Синхронизировать</button></div></Dialog>;
}

function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><section className="admin-dialog" role="dialog" aria-modal="true"><header><h2>{title}</h2><button onClick={onClose}>×</button></header>{children}</section></>;
}
