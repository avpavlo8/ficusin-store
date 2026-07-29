import { FormEvent, useEffect, useRef, useState } from "react";
import { formatRussianPhoneInput, normalizeRussianPhone } from "./lib/phone";

type StoreUser = {
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
};

type AccountOrder = {
  orderNumber: string;
  deliveryMethod: string;
  total: number;
  status: string;
  createdAt: string;
  itemsCount: number;
};

const orderStatusLabels: Record<string, string> = {
  new: "Новый",
  confirmed: "Подтверждён",
  processing: "Собирается",
  shipped: "Передан в доставку",
  completed: "Выполнен",
  cancelled: "Отменён",
};

const money = new Intl.NumberFormat("ru-RU", {
  style: "currency",
  currency: "RUB",
  maximumFractionDigits: 0,
});

const RESEND_COOLDOWN_SECONDS = 45;

function AuthHeader() {
  return (
    <header className="account-header">
      <a className="brand" href="/">
        <span className="brand-mark">⌇</span>
        <span>Фикусин</span>
      </a>
      <a className="account-back" href="/">← Вернуться в магазин</a>
    </header>
  );
}

function AuthPage() {
  const [step, setStep] = useState<"phone" | "code">("phone");
  const [phone, setPhone] = useState("");
  const [accountType, setAccountType] = useState<"retail" | "wholesale">("retail");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const phoneInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = window.setInterval(() => {
      setCooldown((value) => (value > 0 ? value - 1 : 0));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [cooldown]);

  async function requestCode(targetPhone: string) {
    setError("");
    setSubmitting(true);
    try {
      const response = await fetch("/api/v1/auth/request-code", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ phone: targetPhone }),
      });
      const data = (await response.json()) as { error?: string };
      if (!response.ok) {
        throw new Error(data.error || "Не удалось отправить код");
      }
      setCooldown(RESEND_COOLDOWN_SECONDS);
      setStep("code");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось отправить код");
    } finally {
      setSubmitting(false);
    }
  }

  async function submitPhone(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const normalized = normalizeRussianPhone(String(form.get("phone") ?? ""));
    if (!normalized) {
      const input = phoneInputRef.current;
      input?.setCustomValidity("Введите российский номер телефона");
      input?.reportValidity();
      return;
    }
    phoneInputRef.current?.setCustomValidity("");
    setPhone(normalized);
    await requestCode(normalized);
  }

  async function submitCode(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    const form = new FormData(event.currentTarget);
    try {
      const response = await fetch("/api/v1/auth/verify-code", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          phone,
          code: form.get("code"),
          fullName: form.get("fullName"),
          accountType,
        }),
      });
      const data = (await response.json()) as { error?: string };
      if (!response.ok) {
        throw new Error(data.error || "Не удалось подтвердить код");
      }
      const returnTo = new URLSearchParams(window.location.search).get("returnTo");
      window.location.assign(
        returnTo?.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/account",
      );
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось подтвердить код");
      setSubmitting(false);
    }
  }

  return (
    <main className="auth-page">
      <AuthHeader />
      <section className="auth-shell auth-shell-compact">
        <div className="auth-intro">
          <p className="eyebrow">Личный кабинет</p>
          <h1>{step === "phone" ? "Вход и регистрация" : "Подтвердите номер"}</h1>
          <p>
            {step === "phone"
              ? "Укажите номер телефона — пароль не нужен, вам позвонят с кодом."
              : `Вам позвонят на ${phone}. Введите последние 4 цифры номера, с которого поступит звонок.`}
          </p>
        </div>

        {step === "phone" && (
          <form className="auth-form" onSubmit={submitPhone}>
            <label>
              Телефон
              <input
                ref={phoneInputRef}
                name="phone"
                autoComplete="tel"
                inputMode="tel"
                required
                maxLength={18}
                placeholder="+7 900 000-00-00"
                onInput={(event) => {
                  event.currentTarget.setCustomValidity("");
                  event.currentTarget.value = formatRussianPhoneInput(
                    event.currentTarget.value,
                  );
                }}
              />
            </label>
            {error && <p className="auth-error" role="alert">{error}</p>}
            <button className="primary-button full" disabled={submitting}>
              {submitting ? "Звоним…" : "Получить звонок"}
            </button>
          </form>
        )}

        {step === "code" && (
          <form className="auth-form" onSubmit={submitCode}>
            <label>
              Последние 4 цифры номера
              <input
                name="code"
                inputMode="numeric"
                autoComplete="one-time-code"
                required
                maxLength={4}
                pattern="\d{4}"
                placeholder="0000"
                autoFocus
              />
            </label>
            <label>
              Имя и фамилия
              <input name="fullName" autoComplete="name" required minLength={2} maxLength={120} />
              <small>Нужно только при первом входе — остальное можно заполнить позже в кабинете</small>
            </label>
            <fieldset>
              <legend>Тип покупателя</legend>
              <div className="account-type-options">
                <label className={accountType === "retail" ? "selected" : ""}>
                  <input
                    type="radio"
                    name="accountType"
                    checked={accountType === "retail"}
                    onChange={() => setAccountType("retail")}
                  />
                  <span>
                    <b>Розничный</b>
                    <small>Заказ от 1 штуки и накопительная скидка</small>
                  </span>
                </label>
                <label className={accountType === "wholesale" ? "selected" : ""}>
                  <input
                    type="radio"
                    name="accountType"
                    checked={accountType === "wholesale"}
                    onChange={() => setAccountType("wholesale")}
                  />
                  <span>
                    <b>Оптовый</b>
                    <small>Реквизиты можно указать позже в кабинете</small>
                  </span>
                </label>
              </div>
            </fieldset>
            <label className="consent-check">
              <input type="checkbox" required />
              <span>
                Я принимаю <a href="/offer" target="_blank">оферту</a> и даю
                согласие на обработку данных по{" "}
                <a href="/privacy" target="_blank">политике конфиденциальности</a>.
              </span>
            </label>
            {error && <p className="auth-error" role="alert">{error}</p>}
            <button className="primary-button full" disabled={submitting}>
              {submitting ? "Проверяем…" : "Подтвердить и войти"}
            </button>
            <p className="auth-switch">
              {cooldown > 0 ? (
                <>Новый звонок можно запросить через {cooldown} с.</>
              ) : (
                <button type="button" className="text-link" onClick={() => void requestCode(phone)}>
                  Позвонить ещё раз
                </button>
              )}
              {" · "}
              <button
                type="button"
                className="text-link"
                onClick={() => {
                  setStep("phone");
                  setError("");
                }}
              >
                Изменить номер
              </button>
            </p>
          </form>
        )}
      </section>
    </main>
  );
}

export const LoginPage = AuthPage;
export const RegisterPage = AuthPage;

export function AccountPage() {
  const [user, setUser] = useState<StoreUser | null>(null);
  const [orders, setOrders] = useState<AccountOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadAccount() {
      try {
        const response = await fetch("/api/v1/auth/me", {
          credentials: "same-origin",
        });
        if (response.status === 401) {
          window.location.assign("/login?returnTo=/account");
          return;
        }
        if (!response.ok) {
          throw new Error("Не удалось загрузить профиль");
        }
        const result = (await response.json()) as { user: StoreUser };
        setUser(result.user);

        const ordersResponse = await fetch("/api/v1/account/orders", {
          credentials: "same-origin",
        });
        if (!ordersResponse.ok) {
          throw new Error("Не удалось загрузить заказы");
        }
        const ordersResult = (await ordersResponse.json()) as {
          orders: AccountOrder[];
        };
        setOrders(ordersResult.orders);
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : "Не удалось загрузить профиль");
      } finally {
        setLoading(false);
      }
    }
    void loadAccount();
  }, []);

  async function logout() {
    await fetch("/api/v1/auth/logout", { method: "POST" });
    window.location.assign("/");
  }

  if (loading) {
    return <main className="account-page" aria-busy="true" />;
  }
  if (!user) {
    return (
      <main className="account-page">
        <AuthHeader />
        <section className="auth-shell">
          <p className="auth-error" role="alert">
            {error || "Не удалось загрузить профиль"}
          </p>
          <a className="primary-button full" href="/login?returnTo=/account">
            Войти снова
          </a>
        </section>
      </main>
    );
  }

  const discount = user.retailDiscountBps / 100;
  return (
    <main className="account-page">
      <AuthHeader />
      <section className="account-shell">
        <aside className="account-sidebar">
          <div className="account-avatar">{user.fullName.trim().charAt(0).toUpperCase() || "Ф"}</div>
          <h1>{user.fullName}</h1>
          <p>
            {user.phone}
            {user.email ? <><br />{user.email}</> : null}
          </p>
          <nav aria-label="Разделы личного кабинета">
            <a className="active" href="#orders">Мои заказы</a>
            <span>Адреса доставки <small>скоро</small></span>
            <span>Избранное <small>скоро</small></span>
          </nav>
          <button className="signout-link" type="button" onClick={() => void logout()}>
            Выйти из аккаунта
          </button>
        </aside>
        <div className="account-content">
          <div className="account-title">
            <div>
              <p className="eyebrow">Личный кабинет</p>
              <h2>Мои заказы</h2>
            </div>
            <span>{user.accountType === "wholesale" ? "Оптовый клиент" : "Розничный клиент"}</span>
          </div>
          <div className="auth-notice">
            <strong>
              {user.accountType === "wholesale"
                ? "Оптовая заявка на проверке"
                : `Персональная скидка: ${discount.toLocaleString("ru-RU")}%`}
            </strong>
            <p>
              {user.accountType === "wholesale"
                ? "После проверки реквизитов мы включим оптовые условия."
                : "Скидка увеличивается автоматически после выполненных заказов."}
            </p>
          </div>
          <section id="orders" className="orders-list">
            {error && <p className="auth-error" role="alert">{error}</p>}
            {orders.length ? (
              orders.map((order) => (
                <article key={order.orderNumber}>
                  <div>
                    <small>Заказ</small>
                    <strong>{order.orderNumber}</strong>
                  </div>
                  <div>
                    <small>Оформлен</small>
                    <span>{new Date(order.createdAt).toLocaleDateString("ru-RU")}</span>
                  </div>
                  <div>
                    <small>Состав</small>
                    <span>{order.itemsCount} шт.</span>
                  </div>
                  <div>
                    <small>{orderStatusLabels[order.status] ?? order.status}</small>
                    <strong>{money.format(order.total)}</strong>
                  </div>
                </article>
              ))
            ) : (
              <div className="orders-empty">
                <span>⌁</span>
                <h3>Заказов пока нет</h3>
                <p>Новые заказы, оформленные в этом аккаунте, появятся здесь.</p>
                <a className="primary-button" href="/#catalog">Перейти в каталог</a>
              </div>
            )}
          </section>
      </div>
    </section>
    </main>
  );
}
