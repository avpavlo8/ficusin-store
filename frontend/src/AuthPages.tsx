import { FormEvent, useEffect, useState } from "react";
import { formatRussianPhoneInput } from "./lib/phone";

type StoreUser = {
  id: number;
  email: string;
  phone: string;
  fullName: string;
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

export function LoginPage() {
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    const form = new FormData(event.currentTarget);
    try {
      const response = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          identifier: form.get("identifier"),
          password: form.get("password"),
        }),
      });
      const data = (await response.json()) as { error?: string };
      if (!response.ok) {
        throw new Error(data.error || "Не удалось войти");
      }
      const returnTo = new URLSearchParams(window.location.search).get("returnTo");
      window.location.assign(
        returnTo?.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/account",
      );
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось войти");
      setSubmitting(false);
    }
  }

  return (
    <main className="auth-page">
      <AuthHeader />
      <section className="auth-shell auth-shell-compact">
        <div className="auth-intro">
          <p className="eyebrow">Личный кабинет</p>
          <h1>Вход</h1>
          <p>Введите email или российский номер телефона и пароль.</p>
        </div>
        <form className="auth-form" onSubmit={submit}>
          <label>
            Телефон или email
            <input
              name="identifier"
              autoComplete="username"
              required
              onInput={(event) => {
                if (
                  !event.currentTarget.value.includes("@") &&
                  /^[+\d\s()-]*$/.test(event.currentTarget.value)
                ) {
                  event.currentTarget.value = formatRussianPhoneInput(
                    event.currentTarget.value,
                  );
                }
              }}
            />
          </label>
          <label>
            Пароль
            <input name="password" type="password" autoComplete="current-password" required />
          </label>
          {error && <p className="auth-error" role="alert">{error}</p>}
          <button className="primary-button full" disabled={submitting}>
            {submitting ? "Входим…" : "Войти"}
          </button>
          <p className="auth-switch">
            Нет аккаунта? <a href="/register">Зарегистрироваться</a>
          </p>
        </form>
      </section>
    </main>
  );
}

export function RegisterPage() {
  const [accountType, setAccountType] = useState<"retail" | "wholesale">("retail");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    const form = new FormData(event.currentTarget);
    try {
      const response = await fetch("/api/v1/auth/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          fullName: form.get("fullName"),
          phone: form.get("phone"),
          email: form.get("email"),
          password: form.get("password"),
          accountType,
          companyName: form.get("companyName"),
          inn: form.get("inn"),
          kpp: form.get("kpp"),
          legalAddress: form.get("legalAddress"),
          consent: form.get("consent") === "on",
        }),
      });
      const data = (await response.json()) as { error?: string };
      if (!response.ok) {
        throw new Error(data.error || "Не удалось зарегистрироваться");
      }
      window.location.assign("/account");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось зарегистрироваться");
      setSubmitting(false);
    }
  }

  return (
    <main className="auth-page">
      <AuthHeader />
      <section className="auth-shell">
        <div className="auth-intro">
          <p className="eyebrow">Аккаунт покупателя</p>
          <h1>Регистрация</h1>
          <p>
            Сохраняйте историю заказов и получайте персональную скидку. Оптовые
            аккаунты становятся активными после проверки реквизитов.
          </p>
        </div>
        <form className="auth-form" onSubmit={submit}>
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
                  <small>Минимальное количество по товару и оплата по счёту</small>
                </span>
              </label>
            </div>
          </fieldset>
          <fieldset>
            <legend>Контактные данные</legend>
            <label>
              Имя и фамилия
              <input name="fullName" autoComplete="name" required minLength={2} maxLength={120} />
            </label>
            <div className="field-grid">
              <label>
                Телефон
                <input
                  name="phone"
                  autoComplete="tel"
                  inputMode="tel"
                  required
                  maxLength={18}
                  placeholder="+7 900 000-00-00"
                  onInput={(event) => {
                    event.currentTarget.value = formatRussianPhoneInput(
                      event.currentTarget.value,
                    );
                  }}
                />
              </label>
              <label>
                Email
                <input name="email" type="email" autoComplete="email" required />
              </label>
            </div>
            <label>
              Пароль
              <input
                name="password"
                type="password"
                autoComplete="new-password"
                required
                minLength={10}
                maxLength={128}
              />
              <small>Не менее 10 символов, минимум одна буква и одна цифра</small>
            </label>
          </fieldset>
          {accountType === "wholesale" && (
            <fieldset>
              <legend>Реквизиты организации</legend>
              <label>
                Название ИП или организации
                <input name="companyName" required maxLength={180} />
              </label>
              <div className="field-grid">
                <label>
                  ИНН
                  <input name="inn" inputMode="numeric" required pattern="\d{10}|\d{12}" maxLength={12} />
                </label>
                <label>
                  КПП, если есть
                  <input name="kpp" inputMode="numeric" pattern="\d{9}" maxLength={9} />
                </label>
              </div>
              <label>
                Юридический адрес
                <input name="legalAddress" maxLength={300} />
              </label>
              <p className="wholesale-note">
                После регистрации заявка получит статус «На проверке». До
                подтверждения покупки будут доступны по розничным условиям.
              </p>
            </fieldset>
          )}
          <label className="consent-check">
            <input name="consent" type="checkbox" required />
            <span>
              Я принимаю <a href="/offer" target="_blank">оферту</a> и даю
              согласие на обработку данных по{" "}
              <a href="/privacy" target="_blank">политике конфиденциальности</a>.
            </span>
          </label>
          {error && <p className="auth-error" role="alert">{error}</p>}
          <button className="primary-button full" disabled={submitting}>
            {submitting ? "Создаём аккаунт…" : "Зарегистрироваться"}
          </button>
          <p className="auth-switch">
            Уже есть аккаунт? <a href="/login">Войти</a>
          </p>
        </form>
      </section>
    </main>
  );
}

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
          <p>{user.phone}<br />{user.email}</p>
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
