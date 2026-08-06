import { Fragment, useCallback, useEffect, useMemo, useState } from "react";
import { StoreHeader, useStoreUser } from "./StoreHeader";

type Role = "owner" | "manager" | "";
type Section = "dashboard" | "products" | "categories" | "orders" | "customers" | "settings";
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
  deliveryMethod: string; deliveryFeePending?: boolean; repackRequested?: boolean;
  paymentMethod?: string; paymentStatus: string; trackNumber?: string;
  status: string; total: number;
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
const paymentLabels: Record<string, string> = {
  pending: "Ожидает оплаты", paid: "Оплачен", on_delivery: "При получении",
  invoice: "По счёту", cancelled: "Оплата отменена",
  payment_provider_pending: "Без онлайн-оплаты", refunded: "Возвращён",
};
const paymentMethodLabels: Record<string, string> = {
  on_delivery: "Оплатит при получении", invoice: "Нужен счёт на организацию",
};
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
  // Set when the operator arrives from a dashboard shortcut, so the target
  // section can open on the right row instead of a blank list.
  const [focusOrder, setFocusOrder] = useState("");
  const [wholesaleOnly, setWholesaleOnly] = useState(false);
  const user = useStoreUser();

  useEffect(() => { api<AdminData>("/api/v1/admin/dashboard").then(setData).catch(setError); }, []);

  const go = (next: Section, options?: { orderNumber?: string; wholesaleOnly?: boolean }) => {
    setFocusOrder(options?.orderNumber || "");
    setWholesaleOnly(Boolean(options?.wholesaleOnly));
    setSection(next);
  };

  if (!data) return <main className="account-page">
    <StoreHeader showTabBar={false} />
    <section className="account-shell"><div className="account-content"><p>{error || "Загружаем панель…"}</p></div></section>
  </main>;

  const can = (permission: string) => data.permissions.includes(permission);
  const initial = data.user.fullName.trim().charAt(0).toUpperCase() || "Ф";
  return (
    <main className="account-page">
      <StoreHeader showTabBar={false} />
      <section className="account-shell">
        <aside className="account-sidebar">
          <div className="account-avatar">
            {user?.avatarUpdatedAt
              ? <img src={`/api/v1/account/avatar?v=${user.avatarUpdatedAt}`} alt="" />
              : <span>{initial}</span>}
          </div>
          <h1>{data.user.fullName}</h1>
          <p>{roleLabel(data.role)}</p>
          <nav>
            <Nav active={section === "dashboard"} onClick={() => go("dashboard")}>Обзор</Nav>
            {can("products.read") && <Nav active={section === "products"} onClick={() => go("products")}>Товары</Nav>}
            {can("products.read") && <Nav active={section === "categories"} onClick={() => go("categories")}>Категории</Nav>}
            {can("orders.read") && <Nav active={section === "orders"} onClick={() => go("orders")}>Заказы</Nav>}
            {can("customers.read") && <Nav active={section === "customers"} onClick={() => go("customers")}>Клиенты</Nav>}
            {data.role === "owner" && <Nav active={section === "settings"} onClick={() => go("settings")}>Настройки</Nav>}
          </nav>
          <a className="account-switch" href="/account">Личный кабинет →</a>
          <a className="account-switch" href="/">Вернуться в магазин →</a>
        </aside>
        <div className="account-content">
          {error && <div className="admin-message error">{error}<button onClick={() => setError("")}>×</button></div>}
          {section === "dashboard" && <Dashboard data={data} onNavigate={go} />}
          {section === "customers" && <Customers can={can} wholesaleOnly={wholesaleOnly} onError={setError} />}
          {section === "orders" && <Orders focusOrder={focusOrder} onError={setError} />}
          {section === "products" && <Products can={can} onError={setError} />}
          {section === "settings" && data.role === "owner" && <Settings onError={setError} />}
          {section === "categories" && <Categories canEdit={can("products.edit")} onError={setError} />}
        </div>
      </section>
    </main>
  );
}

function Nav({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return <button className={active ? "active" : ""} onClick={onClick}>{children}</button>;
}

// Flattens the category tree into the order it reads in: each parent is
// followed by its own children, siblings sorted by sortOrder and then by
// name. Every consumer that lists categories uses this, so the admin sees
// the same order in the tree, in the parent picker and on a product card.
function orderCategoryTree(items: Category[]): { item: Category; depth: number }[] {
  const result: { item: Category; depth: number }[] = [];
  const append = (parentId: number | null, depth: number) => {
    items
      .filter((item) => item.parentId === parentId)
      .sort((left, right) => left.sortOrder - right.sortOrder || left.name.localeCompare(right.name, "ru"))
      .forEach((item) => { result.push({ item, depth }); append(item.id, depth + 1); });
  };
  append(null, 0);
  // Categories whose parent is missing would otherwise vanish from the list.
  items
    .filter((item) => item.parentId !== null && !items.some((parent) => parent.id === item.parentId))
    .sort((left, right) => left.name.localeCompare(right.name, "ru"))
    .forEach((item) => result.push({ item, depth: 0 }));
  return result;
}

// The category list is long, so a product card shows only the current
// choice and reveals the tree on demand instead of an always-open list.
function CategoryPicker({ categories, value, onChange }: {
  categories: Category[];
  value?: number;
  onChange: (value?: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const ordered = orderCategoryTree(categories);
  const selected = categories.find((item) => item.id === value);
  // Only branches the user opened are listed, so the picker shows a handful
  // of sections instead of every leaf in the catalogue.
  const visible = ordered.filter(({ item }) => {
    let parent = item.parentId;
    while (parent) {
      if (!expanded.has(parent)) return false;
      parent = categories.find((candidate) => candidate.id === parent)?.parentId ?? null;
    }
    return true;
  });
  const toggle = (id: number) => setExpanded((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });
  return <div className="category-picker">
    <button type="button" className="category-picker-toggle" aria-expanded={open} onClick={() => setOpen((current) => !current)}>
      <span>{selected ? selected.name : "Не указано"}</span>
      <span aria-hidden="true">{open ? "−" : "+"}</span>
    </button>
    {open && <div className="category-picker-list">
      <button type="button" className={value ? "" : "active"} onClick={() => { onChange(undefined); setOpen(false); }}>Не указано</button>
      {visible.map(({ item, depth }) => {
        const hasChildren = categories.some((candidate) => candidate.parentId === item.id);
        return <div className="category-picker-row" key={item.id} style={{ paddingLeft: depth * 18 }}>
          {hasChildren
            ? <button type="button" className="category-toggle" aria-expanded={expanded.has(item.id)} onClick={() => toggle(item.id)}>{expanded.has(item.id) ? "−" : "+"}</button>
            : <span className="category-toggle placeholder" aria-hidden="true" />}
          <button
            type="button"
            className={value === item.id ? "active" : ""}
            onClick={() => { onChange(item.id); setOpen(false); }}
          >
            {item.name}
          </button>
        </div>;
      })}
    </div>}
  </div>;
}

function Categories({ canEdit, onError }: { canEdit: boolean; onError: (value: string) => void }) {
  const [items, setItems] = useState<Category[]>([]);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [parentId, setParentId] = useState("");
  const load = useCallback(() => api<{ categories: Category[] }>("/api/v1/admin/categories").then((data) => setItems(data.categories)).catch((error) => onError(error.message)), [onError]);
  useEffect(() => { void load(); }, [load]);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const ordered = orderCategoryTree(items);
  const orderedItems = ordered.map((entry) => entry.item);
  const depth = (item: Category) => ordered.find((entry) => entry.item.id === item.id)?.depth ?? 0;
  // Only rows whose whole chain of parents is expanded are shown; the tree
  // therefore starts collapsed to the root sections.
  const visibleItems = orderedItems.filter((item) => {
    let parent = item.parentId;
    while (parent) {
      if (!expanded.has(parent)) return false;
      parent = items.find((candidate) => candidate.id === parent)?.parentId ?? null;
    }
    return true;
  });
  const toggle = (id: number) => setExpanded((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });
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
    <div className="admin-toolbar category-expand">
      <button className="admin-action" onClick={() => setExpanded(new Set(items.map((item) => item.id)))}>Раскрыть всё</button>
      <button className="admin-action" onClick={() => setExpanded(new Set())}>Свернуть всё</button>
    </div>
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Категория</th><th>Slug</th><th>Товары</th><th /></tr></thead><tbody>{visibleItems.map((item) => {
      const hasChildren = items.some((candidate) => candidate.parentId === item.id);
      return <tr key={item.id} className={canEdit ? "clickable" : ""} onClick={() => { if (canEdit) rename(item); }}><td><strong style={{ paddingLeft: depth(item) * 24 }}>
        {hasChildren
          ? <button className="category-toggle" aria-expanded={expanded.has(item.id)} onClick={(event) => { event.stopPropagation(); toggle(item.id); }}>{expanded.has(item.id) ? "−" : "+"}</button>
          : <span className="category-toggle placeholder" aria-hidden="true" />}
        {item.name}
      </strong></td><td><code>{item.slug}</code></td><td>{item.productsCount}</td><td>{canEdit && <button className="text-button danger" onClick={(event) => { event.stopPropagation(); remove(item); }}>Удалить</button>}</td></tr>;
    })}</tbody></table></div>
  </>;
}


// Shares the account page's heading block so both areas read identically.
function PageHeading({ eyebrow, title, text }: { eyebrow: string; title: string; text: string }) {
  return <div className="account-title"><div><p className="eyebrow">{eyebrow}</p><h2>{title}</h2><p className="account-title-note">{text}</p></div></div>;
}

function Dashboard({ data, onNavigate }: {
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

function Customers({ can, wholesaleOnly, onError }: { can: (permission: string) => boolean; wholesaleOnly?: boolean; onError: (value: string) => void }) {
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
    {form.adminRole === "owner" ? <label>Роль в панели управления<input value="Владелец — назначен через секрет" disabled /></label> : <label>Роль в панели управления<select value={form.adminRole} onChange={(event) => setForm({ ...form, adminRole: event.target.value as Role })}>{roles.map((role) => <option key={role.value} value={role.value}>{role.label}</option>)}</select></label>}
    <label className="admin-checkbox"><input type="checkbox" checked={form.active} onChange={(event) => setForm({ ...form, active: event.target.checked })} />Аккаунт активен</label></>}
  </div><section className="customer-orders"><h3>Заказы клиента</h3>{orders.length === 0 ? <p>Заказов пока нет.</p> : orders.slice(0, 10).map((order) => <article key={order.id}><div><strong>{order.orderNumber}</strong><small>{new Date(order.createdAt).toLocaleDateString("ru-RU")}</small></div><span>{money.format(order.total)}</span><b>{statusLabels[order.status] || order.status}</b></article>)}</section><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" onClick={save}>Сохранить</button></div></Dialog>;
}

function Orders({ focusOrder, onError }: { focusOrder?: string; onError: (value: string) => void }) {
  const [items, setItems] = useState<Order[]>([]);
  const [opened, setOpened] = useState<number | null>(null);
  useEffect(() => {
    api<{ orders: Order[] }>("/api/v1/admin/orders").then((data) => {
      setItems(data.orders);
      // Arriving from "последние заказы": open the row that was clicked.
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
  // Cancelling an order and giving the money back are separate decisions:
  // an order can be cancelled for someone who paid at the counter, and a
  // refund is real money leaving, so it is asked for explicitly.
  const refund = async (order: Order) => {
    if (!window.confirm(`Вернуть ${money.format(order.total)} по заказу ${order.orderNumber}?`)) return;
    try {
      const result = await api<{ order?: Order }>(`/api/v1/admin/orders/${order.id}`, { method: "PATCH", body: JSON.stringify({ refund: true }) });
      if (result.order) setItems((current) => current.map((item) => item.id === order.id ? result.order as Order : item));
    } catch (error) { onError((error as Error).message); }
  };
  // Finishes an order the shop could not price itself. Once the fee is set,
  // the customer gets a notification and can pay from their account.
  const setDeliveryFee = async (order: Order, fee: number) => {
    try {
      const result = await api<{ order: Order }>(`/api/v1/admin/orders/${order.id}`, { method: "PATCH", body: JSON.stringify({ deliveryFee: fee }) });
      setItems((current) => current.map((item) => item.id === order.id ? result.order : item));
    } catch (error) { onError((error as Error).message); }
  };
  return <><PageHeading eyebrow="Продажи" title="Заказы" text="Состав заказа, контакты, доставка, оплата и текущий статус" />
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Заказ</th><th>Клиент</th><th>Получение</th><th>Сумма</th><th>Статус</th><th /></tr></thead><tbody>{items.map((order) => <Fragment key={order.id}>
      <tr className="clickable" onClick={() => setOpened(opened === order.id ? null : order.id)}><td><strong>{order.orderNumber}</strong><small>{new Date(order.createdAt).toLocaleString("ru-RU")}</small></td><td><strong>{order.customerName}</strong><a href={`tel:${order.phone}`} onClick={(event) => event.stopPropagation()}>{order.phone}</a><small>{order.email}</small></td><td><strong>{order.deliveryMethod}</strong><small>{order.address}</small>{order.trackNumber && <small>Трек: {order.trackNumber}</small>}{order.deliveryFeePending && <small className="admin-flag">{order.repackRequested ? "Просят одну коробку — посчитайте доставку" : "Доставку нужно посчитать вручную"}</small>}</td><td><strong>{money.format(order.total)}</strong><small className={order.paymentStatus === "paid" ? "payment-state paid" : order.paymentStatus === "pending" ? "payment-state unpaid" : "payment-state"}>{paymentLabels[order.paymentStatus] ?? order.paymentStatus}</small>{order.paymentMethod && order.paymentMethod !== "online" && <small className="admin-flag">{paymentMethodLabels[order.paymentMethod]}</small>}</td><td onClick={(event) => event.stopPropagation()}><select value={order.status} onChange={(event) => updateStatus(order, event.target.value)}>{orderStatuses.map((status) => <option value={status} key={status}>{statusLabels[status]}</option>)}</select></td><td><span className="admin-row-arrow" aria-hidden="true">{opened === order.id ? "−" : "→"}</span></td></tr>
      {opened === order.id && <tr className="order-details" key={`${order.id}-details`}><td colSpan={6}>{order.paymentStatus === "paid" && <div className="admin-refund"><button onClick={() => refund(order)}>Вернуть деньги покупателю</button><small>Деньги уйдут обратно на карту через ЮKassa. Отменить возврат нельзя.</small></div>}{order.deliveryFeePending && <DeliveryFeeForm order={order} onSubmit={setDeliveryFee} />}<div><strong>Товары</strong>{order.items.map((item) => <p key={`${item.productId}-${item.productName}`}>{item.productName} × {item.quantity} <span>{money.format(item.unitPrice * item.quantity)}</span></p>)}</div><div><strong>Комментарий</strong><p>{order.comment || "Нет комментария"}</p></div></td></tr>}
    </Fragment>)}</tbody></table></div></>;
}

type SettingDefinition = { key: string; title: string; note: string; kind: string };

// The switches the owner flips instead of asking for a redeploy: turning an
// integration off for a test run, the sender details, how long an unpaid
// order waits.
function Settings({ onError }: { onError: (value: string) => void }) {
  const [definitions, setDefinitions] = useState<SettingDefinition[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState("");

  useEffect(() => {
    api<{ definitions: SettingDefinition[]; values: Record<string, string> }>("/api/v1/admin/settings")
      .then((data) => { setDefinitions(data.definitions); setValues(data.values); })
      .catch((error) => onError((error as Error).message));
  }, [onError]);

  const save = async () => {
    try {
      const result = await api<{ values: Record<string, string> }>("/api/v1/admin/settings", { method: "PUT", body: JSON.stringify({ values }) });
      setValues(result.values);
      setSaved("Настройки сохранены и уже действуют");
      window.setTimeout(() => setSaved(""), 3000);
    } catch (error) { onError((error as Error).message); }
  };

  return <><PageHeading eyebrow="Магазин" title="Настройки" text="Действуют сразу, перезапуск не нужен" />
    <div className="admin-settings">
      {definitions.map((definition) => <label key={definition.key} className={definition.kind === "switch" ? "admin-setting switch" : "admin-setting"}>
        {definition.kind === "switch"
          ? <input type="checkbox" checked={values[definition.key] !== "0"} onChange={(event) => setValues({ ...values, [definition.key]: event.target.checked ? "1" : "0" })} />
          : null}
        <span>
          <b>{definition.title}</b>
          <small>{definition.note}</small>
        </span>
        {definition.kind !== "switch"
          ? <input type={definition.kind === "number" ? "number" : "text"} min="0" value={values[definition.key] ?? ""} onChange={(event) => setValues({ ...values, [definition.key]: event.target.value })} />
          : null}
      </label>)}
      <div className="admin-settings-actions">
        <button className="primary" onClick={save}>Сохранить</button>
        {saved && <span>{saved}</span>}
      </div>
    </div>
  </>;
}

// The manager names the delivery price for an order the shop could not
// quote. Kept as its own component so the field holds what is being typed
// without re-rendering the whole order table on every keystroke.
function DeliveryFeeForm({ order, onSubmit }: { order: Order; onSubmit: (order: Order, fee: number) => void }) {
  const [value, setValue] = useState("");
  return <form className="admin-fee-form" onSubmit={(event) => { event.preventDefault(); const fee = Number(value); if (Number.isFinite(fee) && fee >= 0) onSubmit(order, fee); }}>
    <strong>{order.repackRequested ? "Покупатель просит упаковать в одну коробку" : "Доставку нужно посчитать вручную"}</strong>
    <p>Укажите стоимость доставки — покупатель получит уведомление и сможет оплатить заказ.</p>
    <label>Доставка, ₽<input type="number" min="0" step="1" value={value} onChange={(event) => setValue(event.target.value)} placeholder="0" required /></label>
    <button className="primary" type="submit">Сохранить и уведомить</button>
  </form>;
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
    <div className="admin-table-wrap"><table className="admin-table products"><thead><tr><th><input type="checkbox" checked={filtered.length > 0 && filtered.every((item) => selected.includes(item.id))} onChange={(event) => setSelected(event.target.checked ? filtered.map((item) => item.id) : [])} /></th><th>Товар</th><th>Цена / остаток</th><th>Публикация</th><th>СБИС</th><th /></tr></thead><tbody>{filtered.map((product) => <tr
      key={product.id}
      className={can("products.edit") ? "clickable" : ""}
      onClick={() => { if (can("products.edit")) setEditing(product); }}
    >
      <td onClick={(event) => event.stopPropagation()}><input type="checkbox" checked={selected.includes(product.id)} onChange={(event) => setSelected((current) => event.target.checked ? [...current, product.id] : current.filter((id) => id !== product.id))} /></td>
      <td><div className="admin-product"><img src={product.image || "/assets/hero-monstera.png"} alt="" /><div><strong>{product.name}</strong><small>{product.sku} · {product.variantLabel}</small><a href={`/product/${product.slug}`} target="_blank" onClick={(event) => event.stopPropagation()}>Открыть карточку ↗</a></div></div></td>
      <td><strong>{money.format(product.price)}</strong><small>В наличии: {product.stock}</small><small>Опт от {product.wholesaleMinQty} шт.</small></td>
      <td><span className={`admin-pill ${product.status}`}>{statusLabels[product.status] || product.status}</span>{product.overrideFields.length > 0 && <small>Изменено вручную: {product.overrideFields.join(", ")}</small>}</td>
      <td><strong>{product.sabyId ? "Связан" : "Нет связи"}</strong><small>{product.sabyUpdatedAt ? new Date(product.sabyUpdatedAt).toLocaleString("ru-RU") : "Не синхронизировался"}</small>{can("products.sync") && product.sabyId && <button className="text-button" onClick={(event) => { event.stopPropagation(); setSyncing([product.id]); }}>Синхронизировать</button>}</td>
      <td><span className="admin-row-arrow" aria-hidden="true">→</span></td>
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
    <div className="wide admin-field"><span className="admin-field-label">Категория</span><CategoryPicker categories={categories} value={form.categoryId} onChange={(categoryId) => setForm({ ...form, categoryId })} /></div>
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
    <label className="admin-checkbox"><input type="checkbox" checked={form.featured} onChange={(event) => setForm({ ...form, featured: event.target.checked })} />Поднимать в начало каталога</label>
  </div><p className="admin-hint">Габариты упаковки определяют стоимость доставки СДЭК: из коробок всех позиций заказа складывается одна общая. Пока поля пусты, товар считается как коробка 40×25×25 см, 1,5 кг.</p><p className="admin-hint">Ручные изменения защищены от фоновой синхронизации. Вернуть поле к данным СБИС можно кнопкой «Синхронизировать».</p><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" onClick={save}>Сохранить</button></div></Dialog>;
}

function SyncDialog({ count, onClose, onSync }: { count: number; onClose: () => void; onSync: (fields: string[]) => void }) {
  const options = [{ id: "name", label: "Название" }, { id: "photo", label: "Фото" }, { id: "price", label: "Цена" }, { id: "description", label: "Описание" }, { id: "dimensions", label: "Размеры упаковки", disabled: true }];
  const [fields, setFields] = useState(["price"]);
  return <Dialog title={`Синхронизация: ${count} ${count === 1 ? "товар" : "товаров"}`} onClose={onClose}><p>Выбранные поля будут заменены последними данными, полученными из СБИС.</p><div className="sync-options">{options.map((option) => <label className={option.disabled ? "disabled" : ""} key={option.id}><input type="checkbox" disabled={option.disabled} checked={fields.includes(option.id)} onChange={(event) => setFields((current) => event.target.checked ? [...current, option.id] : current.filter((field) => field !== option.id))} /><span><strong>{option.label}</strong>{option.disabled && <small>СБИС пока не отдаёт эти поля в текущем API</small>}</span></label>)}</div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={fields.length === 0} onClick={() => onSync(fields)}>Синхронизировать</button></div></Dialog>;
}

function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><section className="admin-dialog" role="dialog" aria-modal="true"><header><h2>{title}</h2><button onClick={onClose}>×</button></header>{children}</section></>;
}
