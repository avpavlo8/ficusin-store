import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { STORAGE_EVENT, StoreHeader } from "./StoreHeader";
import { PushToggle } from "./PushToggle";
import { discountProgress } from "./account/discount";
import { useSharedCart } from "./lib/cart";

export type StoreUser = {
  id: number;
  email: string;
  phone: string;
  fullName: string;
  lastName: string;
  patronymic: string;
  deliveryAddress: string;
  accountType: "retail" | "wholesale";
  wholesaleStatus: string;
  retailDiscountBps: number;
  lifetimeSpendMinor: number;
  adminRole?: "manager" | "owner";
  avatarUpdatedAt?: string;
};

type AccountOrder = {
  orderNumber: string;
  deliveryMethod: string;
  total: number;
  status: string;
  createdAt: string;
  itemsCount: number;
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
  deliveryFee: number;
  trackNumber?: string;
  hasPreorder?: boolean;
  deliveryFeePending?: boolean;
  repackRequested?: boolean;
  subtotal: number;
  total: number;
  createdAt: string;
  items: Array<{ productName: string; unitPrice: number; quantity: number }>;
};

type FavoriteProduct = {
  id: string; name: string; latin: string; price: number; image: string; stock: number;
};

type Section = "orders" | "profile" | "favorites" | "reviews";

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
  on_delivery: "Оплата при получении",
  invoice: "Счёт от менеджера",
  manager_confirmation: "После подтверждения менеджером",
  cancelled: "Оплата отменена",
  refunded: "Возвращён",
};

const money = new Intl.NumberFormat("ru-RU", {
  style: "currency",
  currency: "RUB",
  maximumFractionDigits: 0,
});

const formatDate = (value: string) =>
  new Date(value).toLocaleDateString("ru-RU", { day: "2-digit", month: "long", year: "numeric" });

function fullNameOf(user: StoreUser) {
  return [user.lastName, user.fullName, user.patronymic].filter(Boolean).join(" ") || user.fullName;
}

const accountIcons: Record<Section, string> = {
  orders: "M5 4h14v16H5zM8 2v4M16 2v4M8 10h8M8 14h5",
  profile: "M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM4 21a8 8 0 0 1 16 0",
  favorites: "M12 21S3 16 3 9a5 5 0 0 1 9-3 5 5 0 0 1 9 3c0 7-9 12-9 12Z",
  reviews: "M4 5h16v12H8l-4 4V5ZM8 9h8M8 13h5",
};

function AccountNavIcon({ name }: { name: Section }) {
  return <svg aria-hidden="true" viewBox="0 0 24 24"><path d={accountIcons[name]} /></svg>;
}

// ---------------------------------------------------------------- shell

/**
 * AccountShell is the frame every account screen shares: the same sidebar,
 * the same heading area, only the panel on the right changes. Order detail
 * reuses it too, so opening an order never feels like leaving the account.
 */
function AccountShell({ user, section, children, onSignOut }: {
  user: StoreUser;
  section: Section;
  children: React.ReactNode;
  onSignOut: () => void;
}) {
  const staff = user.adminRole === "manager" || user.adminRole === "owner";
  const initial = (user.lastName || user.fullName).trim().charAt(0).toUpperCase() || "Ф";
  return (
    <main className="account-page">
      <StoreHeader />
      <section className="account-shell">
        <aside className="account-sidebar">
          <div className="account-avatar">
            {user.avatarUpdatedAt
              ? <img src={`/api/v1/account/avatar?v=${user.avatarUpdatedAt}`} alt="" />
              : <span>{initial}</span>}
          </div>
          <h1>{fullNameOf(user)}</h1>
          <p>
            {user.phone}
            {user.email ? <><br />{user.email}</> : null}
          </p>
          <nav aria-label="Разделы личного кабинета">
            <a className={section === "orders" ? "active" : ""} href="/account"><AccountNavIcon name="orders"/><span>Мои заказы</span></a>
            <a className={section === "profile" ? "active" : ""} href="/account/profile"><AccountNavIcon name="profile"/><span>Мои данные</span></a>
            <a className={section === "favorites" ? "active" : ""} href="/account/favorites"><AccountNavIcon name="favorites"/><span>Избранное</span></a>
            <a className={section === "reviews" ? "active" : ""} href="/account/reviews"><AccountNavIcon name="reviews"/><span>Мои отзывы</span></a>
          </nav>
          <div className="account-help"><strong>Мы рядом,<br/>если нужен совет</strong><p>Поможем с заказом и уходом за растениями.</p><a href="https://max.ru/channel_ficusin" target="_blank" rel="noreferrer">Написать в чат <span>○</span></a></div>
          {staff && <a className="account-switch" href="/admin">Панель управления →</a>}
          <button className="signout-link" type="button" onClick={onSignOut}>
            Выйти из аккаунта
          </button>
        </aside>
        <div className="account-content">{children}</div>
      </section>
    </main>
  );
}

function SectionHeading({ eyebrow, title, aside }: { eyebrow: string; title: string; aside?: React.ReactNode }) {
  return <div className="account-title">
    <div><p className="eyebrow">{eyebrow}</p><h2>{title}</h2></div>
    {aside}
  </div>;
}

// --------------------------------------------------------------- orders

function OrdersSection({ orders, error }: { orders: AccountOrder[]; error: string }) {
  return <>
    <SectionHeading eyebrow="История покупок" title="Мои заказы" />
    {error && <p className="auth-error" role="alert">{error}</p>}
    <section className="orders-list">
      {orders.length ? orders.map((order) => (
        <a className="order-row" key={order.orderNumber} href={`/account/orders/${encodeURIComponent(order.orderNumber)}`}>
          <div><small>Заказ</small><strong>{order.orderNumber}</strong></div>
          <div><small>Оформлен</small><span>{formatDate(order.createdAt)}</span></div>
          <div><small>Состав</small><span>{order.itemsCount} шт.</span></div>
          <div><small>{orderStatusLabels[order.status] ?? order.status}</small><strong>{money.format(order.total)}</strong></div>
          <span className="order-row-arrow" aria-hidden="true">→</span>
        </a>
      )) : (
        <div className="orders-empty">
          <span>⌁</span>
          <h3>Заказов пока нет</h3>
          <p>Новые заказы, оформленные в этом аккаунте, появятся здесь.</p>
          <a className="primary-button" href="/#catalog">Перейти в каталог</a>
        </div>
      )}
    </section>
  </>;
}

function OrderDetailSection({ orderNumber }: { orderNumber: string }) {
  const [order, setOrder] = useState<OrderDetail | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    fetch(`/api/v1/account/orders/${encodeURIComponent(orderNumber)}`, { credentials: "same-origin" })
      .then(async (response) => {
        const data = await response.json() as { order?: OrderDetail; error?: string };
        if (!response.ok || !data.order) throw new Error(data.error || "Заказ не найден");
        setOrder(data.order);
      })
      .catch((caught: Error) => setError(caught.message));
  }, [orderNumber]);

  if (error) return <>
    <SectionHeading eyebrow="Заказ" title={orderNumber} />
    <p className="auth-error" role="alert">{error}</p>
    <a className="text-link" href="/account">← Ко всем заказам</a>
  </>;
  if (!order) return <SectionHeading eyebrow="Заказ" title="Загружаем…" />;

  // Coming back to an unpaid order and paying it later is normal: the card
  // may have been declined, or the manager has only now priced the delivery.
  const payOrder = async (number: string) => {
    try {
      const response = await fetch(`/api/v1/payments/orders/${number}`, {
        method: "POST",
        credentials: "same-origin",
      });
      const result = await response.json() as { confirmationUrl?: string; error?: string };
      if (!response.ok || !result.confirmationUrl) throw new Error(result.error || "Не удалось начать оплату");
      window.location.assign(result.confirmationUrl);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Не удалось начать оплату");
    }
  };

  return <>
    <a className="text-link account-back-link" href="/account">← Ко всем заказам</a>
    <SectionHeading
      eyebrow={`Оформлен ${formatDate(order.createdAt)}`}
      title={`Заказ ${order.orderNumber}`}
      aside={<span>{orderStatusLabels[order.status] ?? order.status}</span>}
    />
    <section className="order-items">
      {order.items.map((item, index) => (
        <div className="order-item" key={`${item.productName}-${index}`}>
          <span>{item.productName}</span>
          <small>{item.quantity} × {money.format(item.unitPrice)}</small>
          <strong>{money.format(item.unitPrice * item.quantity)}</strong>
        </div>
      ))}
    </section>
    <section className="order-totals">
      <div><span>Товары</span><span>{money.format(order.subtotal)}</span></div>
      <div><span>Доставка</span><span>{order.deliveryFeePending ? "рассчитает менеджер" : order.deliveryFee ? money.format(order.deliveryFee) : "—"}</span></div>
      {order.hasPreorder && <p className="order-note">В заказе есть растения под заказ — менеджер уточнит срок поставки и свяжется с вами.</p>}
      {order.deliveryFeePending && <p className="order-note">{order.repackRequested
        ? "Вы просили упаковать растения в одну коробку. Менеджер проверит, поместятся ли они вместе, пересчитает доставку и свяжется с вами. Оплата после подтверждения заказа менеджером."
        : "Менеджер рассчитает стоимость доставки и свяжется с вами до отправки заказа. Оплата после подтверждения заказа менеджером."}</p>}
      <div className="total"><span>Итого</span><span>{money.format(order.total)}</span></div>
    </section>
    <section className="order-facts">
      <div><small>Способ получения</small><span>{deliveryLabels[order.deliveryMethod] ?? order.deliveryMethod}</span></div>
      {order.address && <div><small>Адрес</small><span>{order.address}</span></div>}
      {order.trackNumber && <div><small>Трек-номер СДЭК</small><span><a href={`https://www.cdek.ru/ru/tracking?order_id=${encodeURIComponent(order.trackNumber)}`} target="_blank" rel="noreferrer">{order.trackNumber}</a></span></div>}
      <div><small>Оплата</small><span className={order.paymentStatus === "paid" ? "payment-state paid" : order.paymentStatus === "pending" ? "payment-state unpaid" : "payment-state"}>{paymentLabels[order.paymentStatus] ?? order.paymentStatus}</span></div>
      {order.paymentStatus === "pending" && !order.deliveryFeePending && <button className="primary-button" onClick={() => payOrder(order.orderNumber)}>Оплатить {money.format(order.total)}</button>}
      <div><small>Получатель</small><span>{order.customerName}, {order.phone}</span></div>
      {order.comment && <div><small>Комментарий</small><span>{order.comment}</span></div>}
    </section>
  </>;
}

// -------------------------------------------------------------- profile

type ProfileForm = {
  fullName: string; lastName: string; patronymic: string; email: string; deliveryAddress: string;
};

const profileFormFrom = (user: StoreUser): ProfileForm => ({
  fullName: user.fullName,
  lastName: user.lastName,
  patronymic: user.patronymic,
  email: user.email,
  deliveryAddress: user.deliveryAddress,
});

/**
 * Address suggestions come from our own endpoint, which talks to Yandex
 * with a server-side key. When no key is configured it answers with an
 * empty list and the field behaves like a normal text input.
 */
function AddressField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const skipNextLookup = useRef(false);

  useEffect(() => {
    if (skipNextLookup.current) {
      skipNextLookup.current = false;
      return;
    }
    const controller = new AbortController();
    // Everything, including clearing stale hits, happens on the debounce
    // timer: updating state straight from the effect body would restart the
    // render pass on every keystroke.
    const timer = window.setTimeout(() => {
      if (value.trim().length < 3) {
        setSuggestions([]);
        return;
      }
      fetch(`/api/v1/address/suggest?q=${encodeURIComponent(value)}`, { signal: controller.signal })
        .then((response) => response.json())
        .then((data: { suggestions?: string[] }) => {
          setSuggestions(data.suggestions || []);
          setOpen(true);
        })
        .catch(() => undefined);
    }, 350);
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [value]);

  return <label className="address-field">
    Адрес доставки
    <input
      maxLength={500}
      autoComplete="street-address"
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      onFocus={() => setOpen(suggestions.length > 0)}
      onBlur={() => window.setTimeout(() => setOpen(false), 150)}
    />
    {open && suggestions.length > 0 && <ul className="address-suggestions">
      {suggestions.map((suggestion) => (
        <li key={suggestion}>
          <button type="button" onClick={() => {
            skipNextLookup.current = true;
            onChange(suggestion);
            setOpen(false);
          }}>{suggestion}</button>
        </li>
      ))}
    </ul>}
    <small>Подставим его при оформлении заказа</small>
  </label>;
}

function AvatarEditor({ user, onUpdated, onError }: {
  user: StoreUser;
  onUpdated: (user: StoreUser) => void;
  onError: (message: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);

  // The picture is downscaled in the browser: the server only stores what
  // it receives, so keeping it small here keeps the database small too.
  async function shrink(file: File): Promise<string> {
    const bitmap = await createImageBitmap(file);
    const side = Math.min(bitmap.width, bitmap.height);
    const canvas = document.createElement("canvas");
    canvas.width = 256;
    canvas.height = 256;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("Браузер не смог обработать изображение");
    context.drawImage(
      bitmap,
      (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side,
      0, 0, 256, 256,
    );
    bitmap.close();
    return canvas.toDataURL("image/jpeg", 0.85);
  }

  async function upload(file: File) {
    onError("");
    setBusy(true);
    try {
      const image = await shrink(file);
      const response = await fetch("/api/v1/account/avatar", {
        method: "PUT",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ image }),
      });
      const data = await response.json() as { user?: StoreUser; error?: string };
      if (!response.ok || !data.user) throw new Error(data.error || "Не удалось загрузить фото");
      onUpdated(data.user);
    } catch (caught) {
      onError(caught instanceof Error ? caught.message : "Не удалось загрузить фото");
    } finally {
      setBusy(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  async function remove() {
    onError("");
    setBusy(true);
    try {
      const response = await fetch("/api/v1/account/avatar", {
        method: "DELETE",
        credentials: "same-origin",
      });
      const data = await response.json() as { user?: StoreUser; error?: string };
      if (!response.ok) throw new Error(data.error || "Не удалось удалить фото");
      if (data.user) onUpdated(data.user);
    } catch (caught) {
      onError(caught instanceof Error ? caught.message : "Не удалось удалить фото");
    } finally {
      setBusy(false);
    }
  }

  return <div className="avatar-editor">
    <div className="avatar-preview">
      {user.avatarUpdatedAt
        ? <img src={`/api/v1/account/avatar?v=${user.avatarUpdatedAt}`} alt="Фото профиля" />
        : <span>{(user.lastName || user.fullName).trim().charAt(0).toUpperCase() || "Ф"}</span>}
    </div>
    <div className="avatar-actions">
      <input
        ref={inputRef}
        type="file"
        accept="image/jpeg,image/png,image/webp"
        id="avatar-input"
        onChange={(event) => {
          const file = event.currentTarget.files?.[0];
          if (file) void upload(file);
        }}
      />
      <label className="secondary-button" htmlFor="avatar-input">
        {busy ? "Загружаем…" : user.avatarUpdatedAt ? "Заменить фото" : "Загрузить фото"}
      </label>
      {user.avatarUpdatedAt && (
        <button className="text-link" type="button" disabled={busy} onClick={() => void remove()}>
          Удалить
        </button>
      )}
      <small>JPEG, PNG или WebP. Обрежем по центру до квадрата.</small>
    </div>
  </div>;
}

function ProfileSection({ user, onUpdated }: { user: StoreUser; onUpdated: (user: StoreUser) => void }) {
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState<ProfileForm>(() => profileFormFrom(user));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  const discount = user.retailDiscountBps / 100;
  const progress = discountProgress(user.lifetimeSpendMinor);
  // Владелец может выдать скидку больше заработанной. Тогда путь до
  // следующей ступени показывать нельзя: «персональная скидка 15%» и «до
  // 3% осталось» рядом выглядят как ошибка магазина.
  const earnedBySpending = discount <= progress.percent;

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSaved(false);
    setSaving(true);
    try {
      const response = await fetch("/api/v1/account/profile", {
        method: "PUT",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(form),
      });
      if (response.status === 401) { window.location.assign("/login?returnTo=/account/profile"); return; }
      const data = await response.json() as { user?: StoreUser; error?: string };
      if (!response.ok || !data.user) throw new Error(data.error || "Не удалось сохранить профиль");
      onUpdated(data.user);
      setForm(profileFormFrom(data.user));
      setEditing(false);
      setSaved(true);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось сохранить профиль");
    } finally {
      setSaving(false);
    }
  }

  return <>
    <SectionHeading
      eyebrow="Профиль покупателя"
      title="Мои данные"
      aside={<span>{user.accountType === "wholesale" ? "Оптовый клиент" : "Розничный клиент"}</span>}
    />

    <div className="auth-notice profile-discount">
      <strong>
        {user.accountType === "wholesale"
          ? "Оптовая заявка на проверке"
          : `Персональная скидка: ${discount.toLocaleString("ru-RU")}%`}
      </strong>
      <p>
        {user.accountType === "wholesale"
          ? "После проверки реквизитов мы включим оптовые условия."
          : !earnedBySpending
            ? "Скидка увеличивается автоматически после выполненных заказов."
            : progress.nextPercent
              ? `До ${progress.nextPercent}% осталось выполненных заказов на ${money.format(progress.remainingMinor / 100)}.`
              : "Это наибольшая скидка магазина — дальше расти некуда."}
      </p>
    </div>

    <AvatarEditor user={user} onUpdated={onUpdated} onError={setError} />

    {!editing ? <>
      <dl className="profile-card">
        <div><dt>Имя</dt><dd>{user.fullName || "—"}</dd></div>
        <div><dt>Фамилия</dt><dd>{user.lastName || "—"}</dd></div>
        <div><dt>Отчество</dt><dd>{user.patronymic || "—"}</dd></div>
        <div><dt>Телефон</dt><dd>{user.phone}</dd></div>
        <div><dt>Электронная почта</dt><dd>{user.email || "—"}</dd></div>
        <div><dt>Адрес доставки</dt><dd>{user.deliveryAddress || "—"}</dd></div>
      </dl>
      {error && <p className="auth-error" role="alert">{error}</p>}
      {saved && !error && <p className="auth-saved" role="status">Данные сохранены</p>}
      <button className="primary-button" type="button" onClick={() => { setForm(profileFormFrom(user)); setSaved(false); setEditing(true); }}>
        Изменить
      </button>
      <PushToggle />
    </> : (
      <form className="auth-form profile-form" onSubmit={save}>
        <label>Имя
          <input required minLength={2} maxLength={120} autoComplete="given-name"
            value={form.fullName}
            onChange={(event) => setForm({ ...form, fullName: event.currentTarget.value })} />
        </label>
        <label>Фамилия
          <input maxLength={120} autoComplete="family-name"
            value={form.lastName}
            onChange={(event) => setForm({ ...form, lastName: event.currentTarget.value })} />
        </label>
        <label>Отчество
          <input maxLength={120} autoComplete="additional-name"
            value={form.patronymic}
            onChange={(event) => setForm({ ...form, patronymic: event.currentTarget.value })} />
        </label>
        <label>Электронная почта
          <input type="email" maxLength={254} autoComplete="email"
            value={form.email}
            onChange={(event) => setForm({ ...form, email: event.currentTarget.value })} />
          <small>Нужна для чеков и уведомлений о заказе</small>
        </label>
        <AddressField value={form.deliveryAddress} onChange={(deliveryAddress) => setForm({ ...form, deliveryAddress })} />
        <label>Телефон
          <input value={user.phone} readOnly disabled />
          <small>Телефон — логин аккаунта, изменить его нельзя</small>
        </label>
        {error && <p className="auth-error" role="alert">{error}</p>}
        <div className="form-actions">
          <button className="primary-button" disabled={saving}>{saving ? "Сохраняем…" : "Сохранить"}</button>
          <button className="secondary-button" type="button" onClick={() => { setEditing(false); setError(""); }}>
            Отмена
          </button>
        </div>
      </form>
    )}
  </>;
}

// ------------------------------------------------------------ favorites

function FavoritesSection() {
  const [products, setProducts] = useState<FavoriteProduct[]>([]);
  const [favorites, setFavorites] = useState<Set<string>>(() => {
    try { return new Set(JSON.parse(localStorage.getItem("ficusin-favorites") || "[]") as string[]); }
    catch { return new Set(); }
  });
  const [cart, setCart] = useSharedCart();

  useEffect(() => {
    fetch("/api/v1/catalog", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { products?: FavoriteProduct[] }) => setProducts(data.products || []))
      .catch(() => undefined);
  }, []);

  const items = useMemo(
    () => products.filter((product) => favorites.has(product.id)),
    [products, favorites],
  );

  const remove = (id: string) => {
    const next = new Set(favorites);
    next.delete(id);
    setFavorites(next);
    localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
    // Tell the header to re-read the counter without a page reload.
    window.dispatchEvent(new Event(STORAGE_EVENT));
  };

  return <>
    <SectionHeading eyebrow="Сохранённые товары" title="Избранное" />
    {items.length ? (
      <div className="product-grid">
        {items.map((product) => (
          <article className="product-card" key={product.id}>
            <button className="favorite-button active" onClick={() => remove(product.id)} aria-label="Убрать из избранного">♥</button>
            <a className="product-image" href={`/product/${product.id}`}><img src={product.image} alt={product.name} /></a>
            <h3><a href={`/product/${product.id}`}>{product.name}</a></h3>
            <p>{product.latin}</p>
            <strong>{money.format(product.price)}</strong>
            <button className={cart[product.id] ? "primary-button" : "secondary-button"} type="button" disabled={product.stock <= 0}
              onClick={() => setCart((current) => ({ ...current, [product.id]: Math.min(product.stock, (current[product.id] || 0) + 1) }))}>
              {product.stock <= 0 ? "Нет в наличии" : cart[product.id] ? `Добавить ещё · ${cart[product.id]}` : "В корзину"}
            </button>
          </article>
        ))}
      </div>
    ) : (
      <div className="orders-empty">
        <span>♥</span>
        <h3>В избранном пока пусто</h3>
        <p>Добавляйте товары сердечком в каталоге — они появятся здесь.</p>
        <a className="primary-button" href="/#catalog">Перейти в каталог</a>
      </div>
    )}
  </>;
}

// ----------------------------------------------------------------- page

type AccountReview = { id: number; product: string; slug: string; rating: number; text: string; status: "pending" | "published" | "rejected"; createdAt: string };
const reviewStatusLabels = { pending: "На модерации", published: "Опубликован", rejected: "Отклонён" };

function ReviewsSection() {
  const [items,setItems]=useState<AccountReview[]>([]); const [error,setError]=useState(""); const [editing,setEditing]=useState<AccountReview|null>(null); const [saving,setSaving]=useState(false);
  useEffect(()=>{fetch("/api/v1/account/reviews",{credentials:"same-origin"}).then(async(response)=>{const body=await response.json() as {reviews?:AccountReview[];error?:string};if(!response.ok)throw new Error(body.error||"Не удалось загрузить отзывы");setItems(body.reviews||[])}).catch((reason)=>setError(reason instanceof Error?reason.message:"Не удалось загрузить отзывы"))},[]);
  const save=async(event:FormEvent)=>{event.preventDefault();if(!editing)return;setSaving(true);setError("");try{const response=await fetch(`/api/v1/account/reviews/${editing.id}`,{method:"PATCH",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({rating:editing.rating,text:editing.text})});const body=await response.json() as {error?:string};if(!response.ok)throw new Error(body.error||"Не удалось сохранить");setItems((current)=>current.map((item)=>item.id===editing.id?editing:item));setEditing(null)}catch(reason){setError(reason instanceof Error?reason.message:"Не удалось сохранить")}finally{setSaving(false)}};
  return <div className="account-reviews"><div className="account-title"><div><p className="eyebrow">После покупки</p><h1>Мои отзывы</h1></div></div>{error&&<p className="form-error" role="alert">{error}</p>}{items.length?items.map((item)=><article key={item.id}><header><div><a href={`/product/${item.slug}#reviews`}>{item.product}</a><time>{formatDate(item.createdAt)}</time></div><span className={`admin-pill ${item.status}`}>{reviewStatusLabels[item.status]}</span></header><div className="account-review-stars">{"★".repeat(item.rating)}{"☆".repeat(5-item.rating)}</div><p>{item.text}</p>{item.status==="pending"&&<button type="button" onClick={()=>setEditing({...item})}>Редактировать</button>}</article>):<div className="orders-empty"><h2>Отзывов пока нет</h2><p>Оставить отзыв можно на странице товара после выполненного заказа.</p></div>}{editing&&<div className="account-review-dialog" role="dialog" aria-modal="true" aria-label="Редактировать отзыв"><form onSubmit={(event)=>void save(event)}><header><h2>{editing.product}</h2><button type="button" onClick={()=>setEditing(null)} aria-label="Закрыть">×</button></header><div className="account-review-rating" role="radiogroup">{[1,2,3,4,5].map((value)=><button type="button" role="radio" aria-checked={editing.rating===value} aria-label={`${value} из 5`} className={value<=editing.rating?"active":""} onClick={()=>setEditing({...editing,rating:value})} key={value}>★</button>)}</div><label>Текст отзыва<textarea required minLength={10} maxLength={3000} rows={6} value={editing.text} onChange={(event)=>setEditing({...editing,text:event.target.value})}/></label><button className="primary" disabled={saving}>{saving?"Сохраняем…":"Сохранить"}</button></form></div>}</div>;
}

export default function AccountPage({ section, orderNumber }: {
  section: Section;
  orderNumber?: string;
}) {
  const [user, setUser] = useState<StoreUser | null>(null);
  const [orders, setOrders] = useState<AccountOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const response = await fetch("/api/v1/auth/me", { credentials: "same-origin" });
      if (response.status === 401) {
        window.location.assign(`/login?returnTo=${encodeURIComponent(window.location.pathname)}`);
        return;
      }
      if (!response.ok) throw new Error("Не удалось загрузить профиль");
      const result = await response.json() as { user: StoreUser };
      setUser(result.user);

      if (section === "orders" && !orderNumber) {
        const ordersResponse = await fetch("/api/v1/account/orders", { credentials: "same-origin" });
        if (!ordersResponse.ok) throw new Error("Не удалось загрузить заказы");
        const ordersResult = await ordersResponse.json() as { orders: AccountOrder[] };
        setOrders(ordersResult.orders);
      }
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось загрузить профиль");
    } finally {
      setLoading(false);
    }
  }, [section, orderNumber]);

  useEffect(() => { void load(); }, [load]);

  async function signOut() {
    await fetch("/api/v1/auth/logout", { method: "POST" });
    window.location.assign("/");
  }

  if (loading) return <main className="account-page" aria-busy="true" />;
  if (!user) return (
    <main className="account-page">
      <StoreHeader />
      <section className="auth-shell">
        <p className="auth-error" role="alert">{error || "Не удалось загрузить профиль"}</p>
        <a className="primary-button full" href="/login?returnTo=/account">Войти снова</a>
      </section>
    </main>
  );

  return (
    <AccountShell user={user} section={section} onSignOut={() => void signOut()}>
      {orderNumber
        ? <OrderDetailSection orderNumber={orderNumber} />
        : section === "orders" ? <OrdersSection orders={orders} error={error} />
        : section === "profile" ? <ProfileSection user={user} onUpdated={setUser} />
        : section === "reviews" ? <ReviewsSection />
        : <FavoritesSection />}
    </AccountShell>
  );
}
