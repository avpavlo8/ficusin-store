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

type AuthFlow = "login" | "register";

type RegistrationDraft = {
  fullName: string;
  lastName: string;
  patronymic: string;
  email: string;
  deliveryAddress: string;
  accountType: "retail" | "wholesale";
  companyName: string;
  inn: string;
  kpp: string;
  legalAddress: string;
};

const emptyRegistration: RegistrationDraft = {
  fullName: "",
  lastName: "",
  patronymic: "",
  email: "",
  deliveryAddress: "",
  accountType: "retail",
  companyName: "",
  inn: "",
  kpp: "",
  legalAddress: "",
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
const POLL_INTERVAL_MS = 3000;

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

function AuthPage({ flow }: { flow: AuthFlow }) {
  const isRegistration = flow === "register";
  const [step, setStep] = useState<"form" | "waiting">("form");
  const [phone, setPhone] = useState("");
  const [checkId, setCheckId] = useState("");
  const [callPhonePretty, setCallPhonePretty] = useState("");
  const [registration, setRegistration] = useState<RegistrationDraft>(emptyRegistration);
  const [accountType, setAccountType] = useState<"retail" | "wholesale">("retail");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const phoneInputRef = useRef<HTMLInputElement | null>(null);
  const pollStateRef = useRef({
    phone: "",
    checkId: "",
    flow,
    registration: emptyRegistration,
  });
  pollStateRef.current = { phone, checkId, flow, registration };

  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = window.setInterval(() => {
      setCooldown((value) => (value > 0 ? value - 1 : 0));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [cooldown]);

  function completeLogin() {
    const returnTo = new URLSearchParams(window.location.search).get("returnTo");
    window.location.assign(
      returnTo?.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/account",
    );
  }

  useEffect(() => {
    if (step !== "waiting") return;
    let cancelled = false;

    async function poll() {
      const current = pollStateRef.current;
      try {
        const response = await fetch("/api/v1/auth/verify-code", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          credentials: "same-origin",
          body: JSON.stringify({
            phone: current.phone,
            checkId: current.checkId,
            flow: current.flow,
            ...(current.flow === "register" ? current.registration : {}),
          }),
        });
        if (cancelled || response.status === 202) return;
        const data = (await response.json()) as { error?: string };
        if (!response.ok) {
          setError(data.error || "Не удалось подтвердить звонок");
          setStep("form");
          return;
        }
        completeLogin();
      } catch {
        // A short network failure is retried on the next polling tick.
      }
    }

    const timer = window.setInterval(() => void poll(), POLL_INTERVAL_MS);
    void poll();
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [step]);

  async function requestCall(targetPhone: string) {
    setError("");
    setSubmitting(true);
    try {
      const response = await fetch("/api/v1/auth/request-code", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ phone: targetPhone, flow }),
      });
      const data = (await response.json()) as {
        error?: string;
        checkId?: string;
        callPhonePretty?: string;
      };
      if (!response.ok || !data.checkId || !data.callPhonePretty) {
        throw new Error(data.error || "Не удалось подготовить звонок");
      }
      setCheckId(data.checkId);
      setCallPhonePretty(data.callPhonePretty);
      setCooldown(RESEND_COOLDOWN_SECONDS);
      setStep("waiting");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось подготовить звонок");
    } finally {
      setSubmitting(false);
    }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const normalized = normalizeRussianPhone(String(form.get("phone") ?? ""));
    if (!normalized) {
      phoneInputRef.current?.setCustomValidity("Введите российский номер телефона");
      phoneInputRef.current?.reportValidity();
      return;
    }
    phoneInputRef.current?.setCustomValidity("");
    setPhone(normalized);

    if (isRegistration) {
      setRegistration({
        fullName: String(form.get("fullName") ?? "").trim(),
        lastName: String(form.get("lastName") ?? "").trim(),
        patronymic: String(form.get("patronymic") ?? "").trim(),
        email: String(form.get("email") ?? "").trim(),
        deliveryAddress: String(form.get("deliveryAddress") ?? "").trim(),
        accountType,
        companyName: String(form.get("companyName") ?? "").trim(),
        inn: String(form.get("inn") ?? "").trim(),
        kpp: String(form.get("kpp") ?? "").trim(),
        legalAddress: String(form.get("legalAddress") ?? "").trim(),
      });
    }
    await requestCall(normalized);
  }

  return (
    <main className="auth-page">
      <AuthHeader />
      <section className="auth-shell auth-shell-compact">
        <div className="auth-intro">
          <p className="eyebrow">Личный кабинет</p>
          <h1>
            {step === "waiting"
              ? "Подтвердите номер звонком"
              : isRegistration
                ? "Регистрация"
                : "Вход"}
          </h1>
          <p>
            {step === "waiting"
              ? "Позвоните с номера " + phone + " на " + callPhonePretty +
                ". Звонок бесплатный — сервис автоматически его сбросит."
              : isRegistration
                ? "Заполните профиль один раз. При следующих заказах данные подставятся автоматически."
                : "Введите телефон, указанный при регистрации. Пароль не нужен."}
          </p>
        </div>

        {step === "form" && (
          <form className="auth-form" onSubmit={submit}>
            {isRegistration && (
              <>
                <div className="field-grid">
                  <label>
                    Фамилия
                    <input name="lastName" autoComplete="family-name" required maxLength={80} />
                  </label>
                  <label>
                    Имя
                    <input name="fullName" autoComplete="given-name" required minLength={2} maxLength={80} />
                  </label>
                </div>
                <label>
                  Отчество
                  <input name="patronymic" autoComplete="additional-name" maxLength={80} />
                </label>
              </>
            )}
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
                  event.currentTarget.value = formatRussianPhoneInput(event.currentTarget.value);
                }}
              />
            </label>
            {isRegistration && (
              <>
                <label>
                  Email
                  <input name="email" type="email" autoComplete="email" required maxLength={254} />
                </label>
                <label>
                  Адрес доставки
                  <input
                    name="deliveryAddress"
                    autoComplete="street-address"
                    placeholder="Город, улица, дом, квартира"
                  />
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
                        <small>Заказ партиями и оплата по реквизитам</small>
                      </span>
                    </label>
                  </div>
                </fieldset>
                {accountType === "wholesale" && (
                  <div className="wholesale-fields">
                    <label>
                      Название организации
                      <input name="companyName" required maxLength={160} />
                    </label>
                    <div className="field-grid">
                      <label>
                        ИНН
                        <input name="inn" required inputMode="numeric" minLength={10} maxLength={12} />
                      </label>
                      <label>
                        КПП
                        <input name="kpp" inputMode="numeric" minLength={9} maxLength={9} />
                      </label>
                    </div>
                    <label>
                      Юридический адрес
                      <input name="legalAddress" required maxLength={300} />
                    </label>
                  </div>
                )}
                <label className="consent-check">
                  <input type="checkbox" required />
                  <span>
                    Я принимаю <a href="/offer" target="_blank">оферту</a> и даю
                    согласие на обработку данных по{" "}
                    <a href="/privacy" target="_blank">политике конфиденциальности</a>.
                  </span>
                </label>
              </>
            )}
            {error && <p className="auth-error" role="alert">{error}</p>}
            <button className="primary-button full" disabled={submitting}>
              {submitting
                ? "Готовим номер…"
                : isRegistration
                  ? "Зарегистрироваться"
                  : "Войти"}
            </button>
            <p className="auth-switch">
              {isRegistration ? (
                <>Уже зарегистрированы? <a href="/login">Войти</a></>
              ) : (
                <>Нет аккаунта? <a href="/register">Зарегистрироваться</a></>
              )}
            </p>
          </form>
        )}

        {step === "waiting" && (
          <div className="auth-form">
            <p className="auth-call-number" aria-live="polite">
              <b>{callPhonePretty}</b>
              <small>Ожидаем звонок и автоматически завершим {isRegistration ? "регистрацию" : "вход"}…</small>
            </p>
            {error && <p className="auth-error" role="alert">{error}</p>}
            <p className="auth-switch">
              {cooldown > 0 ? (
                <>Новый номер можно запросить через {cooldown} с.</>
              ) : (
                <button type="button" className="text-link" onClick={() => void requestCall(phone)}>
                  Получить новый номер
                </button>
              )}
              {" · "}
              <button
                type="button"
                className="text-link"
                onClick={() => {
                  setStep("form");
                  setError("");
                }}
              >
                Изменить данные
              </button>
            </p>
          </div>
        )}
      </section>
    </main>
  );
}

export function LoginPage() {
  return <AuthPage flow="login" />;
}

export function RegisterPage() {
  return <AuthPage flow="register" />;
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
          <div className="account-avatar">{(user.lastName || user.fullName).trim().charAt(0).toUpperCase() || "Ф"}</div>
          <h1>{[user.lastName, user.fullName, user.patronymic].filter(Boolean).join(" ")}</h1>
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
