import "./styles/customer-pages.css";
import { usePageTitle } from "./lib/usePageTitle";
import { useFavorites } from "./lib/favorites";
import { lazy, Suspense, type CSSProperties, FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { StoreHeader } from "./StoreHeader";
import { StoreNavIllustration } from "./StoreNavIllustration";
import { PushToggle } from "./PushToggle";
import { useSharedCart } from "./lib/cart";
import { sharedAPIJSON } from "./lib/api";
import { money } from "./lib/money";
import { useChatUnread } from "./lib/chat";
const AccountChat = lazy(() => import("./AccountChat"));
import AccountReviews from "./AccountReviews";
import { CatalogProductCard, type CatalogCardProduct } from "./CatalogProductCard";

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
  adminRole?: "manager" | "administrator" | "owner";
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
  deliveryPayee?: "shop" | "carrier";
  trackNumber?: string;
  hasPreorder?: boolean;
  deliveryFeePending?: boolean;
  paymentReady?: boolean;
  repackRequested?: boolean;
  subtotal: number;
  discountAmount?: number;
  total: number;
  createdAt: string;
  items: Array<{ productName: string; unitPrice: number; quantity: number }>;
};

type FavoriteProduct = CatalogCardProduct;

type Section = "chat" | "orders" | "profile" | "favorites" | "reviews";

const orderStatusLabels: Record<string, string> = {
  new: "Новый",
  confirmed: "Подтверждён",
  assembling: "Собирается",
  processing: "Собирается",
  ready: "Готов к выдаче",
  shipped: "В доставке",
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

// Ступени скидки повторяют backend/internal/order/loyalty.go: каждые
// 10 000 ₽ выполненных заказов — один процент, потолок — десять процентов.
// Кабинет обещал, что скидка растёт, но не говорил, сколько до следующей
// ступени, и обещание нечем было проверить.
const stepSpend = 10_000;
const maxDiscountPercent = 10;

function discountLadder(spendMinor: number, currentBps: number) {
  const spend = Math.max(0, spendMinor) / 100;
  const earned = Math.min(maxDiscountPercent, Math.floor(spend / stepSpend));
  if (earned >= maxDiscountPercent) return { top: true as const };
  const next = earned + 1;
  return {
    top: false as const,
    next,
    spend,
    remaining: next * stepSpend - spend,
    // Владелец может назначить скидку больше накопленной. Тогда следующая
    // ступень ничего не изменит, и обещать её было бы обманом.
    raises: next * 100 > currentBps,
  };
}

const formatDate = (value: string) =>
  new Date(value).toLocaleDateString("ru-RU", { day: "2-digit", month: "long", year: "numeric" });

function fullNameOf(user: StoreUser) {
  return [user.lastName, user.fullName, user.patronymic].filter(Boolean).join(" ") || user.fullName;
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
  const chatUnread = useChatUnread();
  const staff = user.adminRole === "manager" || user.adminRole === "administrator" || user.adminRole === "owner";
  const firstName = user.fullName.trim().split(/\s+/)[0] || "Друг";
  const initial = firstName.charAt(0).toUpperCase();
  return (
    <main className={`account-page customer-account ${section === "chat" ? "account-chat-page" : ""}`}>
      <StoreHeader showTabBar={section !== "chat"} />
      <section className="account-shell">
        <aside className="account-sidebar cozy-account-menu" aria-label="Ваш личный кабинет">
          <div className="cozy-account-identity">
            <div className="account-avatar">
              {user.avatarUpdatedAt ? <img src={`/api/v1/account/avatar?v=${user.avatarUpdatedAt}`} alt="" /> : <span>{initial}</span>}
            </div>
            <div><p className="account-person-name" title={fullNameOf(user)}>{firstName},<br /> рады вам!</p></div>
          </div>
          <nav aria-label="Разделы личного кабинета">
            {([
              { section: "chat", href: "/account/chat", label: "Сообщения", note: "Переписка с магазином", icon: "messages" },
              { section: "orders", href: "/account", label: "Мои заказы", note: "Ваши зелёные покупки", icon: "basket" },
              { section: "profile", href: "/account/profile", label: "Мои данные", note: "Всё о вас — под рукой", icon: "person" },
              { section: "favorites", href: "/account/favorites", label: "Избранное", note: "Приглянулись и ждут", icon: "heart" },
              { section: "reviews", href: "/account/reviews", label: "Мои отзывы", note: "Впечатления о покупках", icon: "reviews" },
            ] as const).map(item => <a key={item.section} aria-current={section === item.section ? "page" : undefined} className={section === item.section ? "active" : ""} href={item.href}>
              <span className="cozy-nav-icon"><StoreNavIllustration kind={item.icon} /></span>
              <span className="cozy-nav-copy"><strong>{item.label}{item.section === "chat" && chatUnread > 0 && <b className="account-chat-count" aria-label={`Непрочитанных: ${chatUnread}`}>{chatUnread}</b>}</strong><small>{item.note}</small></span>
              <svg className="cozy-nav-arrow" viewBox="0 0 20 20" aria-hidden="true"><path d="m7 5 5 5-5 5" /></svg>
            </a>)}
          </nav>
          <a className="cozy-care-link" href="/care">Подсказки по уходу <span aria-hidden="true">↗</span></a>
          <div className="cozy-account-footer">
            {staff && <a className="account-switch" href="/admin">Управление магазином ↗</a>}
            <button className="signout-link" type="button" aria-label="Выйти из аккаунта" onClick={onSignOut}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M8 3H4v14h4m3-11 4 4-4 4m-4-4h8" /></svg>Выйти</button>
          </div>
        </aside>
        <div className="account-content">{children}</div>
      </section>
    </main>
  );
}

function SectionHeading({ title, aside }: { eyebrow: string; title: string; aside?: React.ReactNode }) {
  return <div className="account-title">
    <div><h1>{title}</h1></div>
    {aside}
  </div>;
}

// --------------------------------------------------------------- orders

function OrdersSection({ orders, error, onRetry }: { orders: AccountOrder[]; error: string; onRetry: () => void }) {
  return <>
    <SectionHeading eyebrow="История покупок" title="Мои заказы" />
    {error && <div className="auth-error" role="alert"><p>{error}</p><button type="button" className="secondary-button" onClick={onRetry}>Повторить загрузку</button></div>}
    {!error && <section className="orders-list">
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
          <h3>Заказов пока нет</h3>
          <p>Новые заказы, оформленные в этом аккаунте, появятся здесь.</p>
          <a className="primary-button" href="/#catalog">Перейти в каталог</a>
        </div>
      )}
    </section>}
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

  // The manager can make an unpaid order payable after checking availability.
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
      <div><span>Товары</span><span>{money.format(order.subtotal + (order.discountAmount ?? 0))}</span></div>
      {!!order.discountAmount && <div><span>Скидка</span><span>−{money.format(order.discountAmount)}</span></div>}
      <div><span>{order.deliveryPayee === "carrier" ? "Доставка, справочно" : "Доставка"}</span><span>{order.deliveryFeePending ? "рассчитает менеджер" : order.deliveryFee ? money.format(order.deliveryFee) : "—"}</span></div>
      {order.hasPreorder && <p className="order-note">В заказе есть растения под заказ. Менеджер уточнит срок поставки; до его решения оплата не требуется.</p>}
      {order.deliveryFeePending && <p className="order-note">{order.deliveryPayee === "carrier" ? (order.repackRequested
        ? "Вы просили упаковать растения в одну коробку. Менеджер уточнит стоимость доставки до отправки."
        : "Менеджер уточнит стоимость доставки до отправки.") : "Менеджер уточнит стоимость доставки и сообщит итоговую сумму заказа."}</p>}
      {order.deliveryPayee === "carrier" && <p className="order-note">Доставка оплачивается перевозчику при получении.</p>}
      <div className="total"><span>{order.deliveryPayee === "carrier" ? "Растения к оплате" : "Итого"}</span><span>{money.format(order.total)}</span></div>
    </section>
    <section className="order-facts">
      <div><small>Способ получения</small><span>{deliveryLabels[order.deliveryMethod] ?? order.deliveryMethod}</span></div>
      {order.address && <div><small>Адрес</small><span>{order.address}</span></div>}
      {order.trackNumber && <div><small>Трек-номер СДЭК</small><span><a href={`https://www.cdek.ru/ru/tracking?order_id=${encodeURIComponent(order.trackNumber)}`} target="_blank" rel="noreferrer">{order.trackNumber}</a></span></div>}
      <div><small>Оплата</small><span className={order.paymentStatus === "paid" ? "payment-state paid" : order.status !== "cancelled" && order.paymentStatus === "pending" ? "payment-state unpaid" : "payment-state"}>{order.status === "cancelled" && ["pending", "manager_confirmation", "payment_provider_pending"].includes(order.paymentStatus) ? "Оплата не требуется" : paymentLabels[order.paymentStatus] ?? order.paymentStatus}</span></div>
      {order.status !== "cancelled" && order.paymentStatus === "pending" && order.paymentReady === true && !order.hasPreorder && !order.deliveryFeePending && <button className="primary-button" onClick={() => payOrder(order.orderNumber)}>{order.deliveryPayee === "carrier" ? "Оплатить растения" : "Оплатить заказ"}: {money.format(order.total)}</button>}
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
    if (!["image/jpeg", "image/png", "image/webp"].includes(file.type)) throw new Error("Выберите фото JPEG, PNG или WebP");
    if (file.size > 10 * 1024 * 1024) throw new Error("Фото должно быть меньше 10 МБ");
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
        hidden
        disabled={busy}
        onChange={(event) => {
          const file = event.currentTarget.files?.[0];
          if (file) void upload(file);
        }}
      />
      <button className="secondary-button avatar-upload-button" type="button" disabled={busy} onClick={() => inputRef.current?.click()}>
        {busy ? "Загружаем…" : user.avatarUpdatedAt ? "Заменить фото" : "Загрузить фото"}
      </button>
      {user.avatarUpdatedAt && (
        <button className="text-link" type="button" disabled={busy} onClick={() => void remove()}>
          Удалить
        </button>
      )}
    </div>
  </div>;
}

function ProfileSection({ user, onUpdated }: { user: StoreUser; onUpdated: (user: StoreUser) => void }) {
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState<ProfileForm>(() => profileFormFrom(user));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [loyaltyRulesOpen, setLoyaltyRulesOpen] = useState(false);

  const discount = user.retailDiscountBps / 100;
  const ladder = discountLadder(user.lifetimeSpendMinor, user.retailDiscountBps);
  const levelProgress = ladder.top || !ladder.raises ? null : Math.min(100, Math.max(0,
    (ladder.spend - discount * stepSpend) / ((ladder.next - discount) * stepSpend) * 100,
  ));

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
      aside={user.accountType === "wholesale" ? <span>Оптовый клиент</span> : undefined}
    />

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
      {user.accountType === "wholesale" ? <div className="auth-notice wholesale-conditions">
        <strong>{user.wholesaleStatus === "approved" ? "Оптовые условия одобрены" : user.wholesaleStatus === "rejected" ? "Оптовая заявка отклонена" : "Оптовая заявка на проверке"}</strong>
        <p>{user.wholesaleStatus === "approved" ? "Условия для вашего аккаунта согласованы менеджером." : user.wholesaleStatus === "rejected" ? "Свяжитесь с магазином, чтобы уточнить условия и реквизиты." : "После проверки реквизитов мы включим оптовые условия."}</p>
      </div> : <section className="loyalty-card" aria-labelledby="loyalty-title">
        <div className="loyalty-copy">
          <h2 id="loyalty-title">Персональная скидка</h2>
          {levelProgress === null && <p className="loyalty-value">{discount.toLocaleString("ru-RU")}%</p>}
          <button className="loyalty-info" type="button" aria-label="Условия персональной скидки" aria-expanded={loyaltyRulesOpen} aria-controls="loyalty-rules" onClick={() => setLoyaltyRulesOpen(open => !open)}>
            <svg viewBox="0 0 20 20" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true"><circle cx="10" cy="10" r="7.5" /><path d="M10 9v5M10 5.5v1" /></svg>
          </button>
        </div>
        {levelProgress !== null && !ladder.top && <div className="loyalty-progress">
          <span aria-hidden="true">{discount.toLocaleString("ru-RU")}%</span>
          <div className="loyalty-meter" role="meter" aria-label="Персональная скидка" aria-valuemin={0} aria-valuemax={100} aria-valuenow={levelProgress} aria-valuetext={`От ${discount.toLocaleString("ru-RU")}% до ${ladder.next}%: пройдено ${Math.round(levelProgress)}% пути`} style={{ "--loyalty-fill": `${levelProgress}%` } as CSSProperties}>
            <span aria-hidden="true" />
          </div>
          <span aria-hidden="true">{ladder.next}%</span>
        </div>}
        <p className="loyalty-next">{ladder.top
            ? <>Вы на верхней ступени программы. Спасибо, что выбираете нас!</>
            : ladder.raises
              ? <>Ещё <strong>{money.format(ladder.remaining)}</strong> до следующего уровня</>
              : <>Скидка назначена вручную и выше накопленной — она не уменьшится.</>}</p>
        <div className="loyalty-rules" id="loyalty-rules" hidden={!loyaltyRulesOpen}>
          <p>Скидка зависит от общей суммы выполненных заказов.</p>
          <table>
            <thead><tr><th scope="col">Сумма покупок</th><th scope="col">Скидка</th></tr></thead>
            <tbody>{Array.from({ length: maxDiscountPercent }, (_, index) => index + 1).map(level => <tr key={level}><td>От {money.format(level * stepSpend)}</td><td>{level}%</td></tr>)}</tbody>
          </table>
          <p>Скидка растёт на 1% за каждые {money.format(stepSpend)}, максимум — {maxDiscountPercent}%. Назначенная вручную скидка не уменьшается.</p>
        </div>
      </section>}
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
          <button className="secondary-button" type="button" disabled={saving} onClick={() => { setEditing(false); setError(""); }}>
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
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [favorites, setFavorites] = useFavorites();
  const [cart, setCart] = useSharedCart();
  const load = useCallback((force = false) => {
    void sharedAPIJSON<{ products?: FavoriteProduct[] }>("/api/v1/catalog", force)
      .then(data => { setProducts(data.products || []); setStatus("ready"); })
      .catch(() => setStatus("error"));
  }, []);
  useEffect(() => { load(); }, [load]);
  const items = useMemo(() => products.filter(product => favorites.has(String(product.id))), [products, favorites]);
  const unavailable = [...favorites].filter(id => !products.some(product => String(product.id) === id));
  return <>
    <SectionHeading eyebrow="Сохранённые товары" title="Избранное" />
    <p className="customer-intro">Сохранённые растения на этом устройстве. Список общий с избранным в каталоге.</p>
    {status === "loading" && <p role="status">Загружаем избранное…</p>}
    {status === "error" && <div className="auth-error" role="alert"><p>Не удалось загрузить избранное. Сохранённые товары на месте.</p><button type="button" className="secondary-button" onClick={() => { setStatus("loading"); load(true); }}>Повторить загрузку</button></div>}
    {status === "ready" && unavailable.length > 0 && <p className="favorites-unavailable">Некоторые сохранённые товары сейчас недоступны. <button type="button" onClick={() => setFavorites(current => new Set([...current].filter(id => !unavailable.includes(id))))}>Убрать недоступные</button></p>}
    {status === "ready" && <div className="storefront-grid customer-favorites-grid">{items.map(product => <CatalogProductCard key={product.id} product={product} favorite inCart={cart[product.sku] || 0}
      onFavorite={() => setFavorites(current => { const next = new Set(current); next.delete(String(product.id)); return next; })}
      onAdd={() => setCart(current => ({ ...current, [product.sku]: Math.min(product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20, (current[product.sku] || 0) + 1) }))}
      onDecrease={() => setCart(current => { const next = { ...current }; if ((next[product.sku] || 0) <= 1) delete next[product.sku]; else next[product.sku] -= 1; return next; })} />)}</div>}
    {status === "ready" && !favorites.size && <div className="orders-empty"><h2>В избранном пока пусто</h2><p>Добавляйте товары сердечком в каталоге.</p><a className="primary-button" href="/#catalog">Перейти в каталог</a></div>}
  </>;
}

// ----------------------------------------------------------------- page

export default function AccountPage({ section, orderNumber }: {
  section: Section;
  orderNumber?: string;
}) {
  const [user, setUser] = useState<StoreUser | null>(null);
  const [orders, setOrders] = useState<AccountOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [signingOut, setSigningOut] = useState(false);
  usePageTitle({ chat: "Сообщения", orders: "Мои заказы", profile: "Мои данные", favorites: "Избранное", reviews: "Мои отзывы" }[section]);

  const load = useCallback(async () => {
    setError("");
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
        setOrders(ordersResult.orders || []);
      }
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось загрузить профиль");
    } finally {
      setLoading(false);
    }
  }, [section, orderNumber]);

  useEffect(() => { void load(); }, [load]);

  async function signOut() {
    if (signingOut) return;
    setSigningOut(true);
    try {
      const response = await fetch("/api/v1/auth/logout", { method: "POST", credentials: "same-origin" });
      if (!response.ok) throw new Error("Не удалось выйти. Повторите попытку.");
      window.location.assign("/");
    } catch { setError("Не удалось выйти из аккаунта. Проверьте соединение и повторите попытку."); }
    finally { setSigningOut(false); }
  }

  if (loading) return <main className="account-page customer-account"><StoreHeader /><section className="customer-loading" aria-busy="true"><h1>Личный кабинет</h1><p role="status">Загружаем ваши данные…</p></section></main>;
  if (!user) return (
    <main className="account-page customer-account">
      <StoreHeader />
      <section className="auth-shell">
        <p className="auth-error" role="alert">{error || "Не удалось загрузить профиль"}</p>
        <button type="button" className="primary-button" onClick={() => void load()}>Повторить загрузку</button><a className="text-link" href={`/login?returnTo=${encodeURIComponent(window.location.pathname)}`}>Войти снова</a>
      </section>
    </main>
  );

  return (
    <AccountShell user={user} section={section} onSignOut={() => void signOut()}>
      {error && section !== "orders" && <p className="auth-error" role="alert">{error}</p>}
      {orderNumber
        ? <OrderDetailSection orderNumber={orderNumber} />
        : section === "orders" ? <OrdersSection orders={orders} error={error} onRetry={() => void load()} />
        : section === "profile" ? <ProfileSection user={user} onUpdated={setUser} />
        : section === "chat" ? <Suspense fallback={<p role="status">Открываем сообщения…</p>}><AccountChat /></Suspense>
        : section === "reviews" ? <AccountReviews />
        : <FavoritesSection />}
    </AccountShell>
  );
}
