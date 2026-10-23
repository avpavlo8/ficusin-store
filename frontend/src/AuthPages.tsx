import "./styles/customer-pages.css";
import { usePageTitle } from "./lib/usePageTitle";
import { FormEvent, useCallback, useEffect, useRef, useState } from "react";
import { formatRussianPhoneInput, normalizeRussianPhone } from "./lib/phone";
import { StoreHeader } from "./StoreHeader";
import { OrganizationLookup } from "./OrganizationLookup";

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
  // The server refuses to register anyone without this and writes the
  // agreement to the database, so it has to travel with the form.
  consent: boolean;
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
  consent: false,
};

const POLL_INTERVAL_MS = 3000;

function AuthPage({ flow }: { flow: AuthFlow }) {
  const isRegistration = flow === "register";
  usePageTitle(isRegistration ? "Регистрация" : "Вход");
  const requestedReturn = new URLSearchParams(window.location.search).get("returnTo");
  const returnTo = requestedReturn?.startsWith("/") && !requestedReturn.startsWith("//") ? requestedReturn : "/account";
  const switchQuery = returnTo === "/account" ? "" : `?returnTo=${encodeURIComponent(returnTo)}`;
  const [step, setStep] = useState<"form" | "waiting">("form");
  const [phone, setPhone] = useState("");
  const [checkId, setCheckId] = useState("");
  const [callPhonePretty, setCallPhonePretty] = useState("");
  const [registration, setRegistration] = useState<RegistrationDraft>(emptyRegistration);
  const [accountType, setAccountType] = useState<"retail" | "wholesale">("retail");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const phoneInputRef = useRef<HTMLInputElement | null>(null);
  const pollStateRef = useRef({
    phone: "",
    checkId: "",
    flow,
    registration: emptyRegistration,
  });
  pollStateRef.current = { phone, checkId, flow, registration };

  const completeLogin = useCallback(() => {
    const target = new URL(returnTo, window.location.origin);
    if (target.origin === window.location.origin) {
      window.location.assign(target.href);
    } else {
      window.location.assign("/account");
    }
  }, [returnTo]);

  useEffect(() => {
    if (step !== "waiting") return;
    let cancelled = false;
    let inFlight = false;

    async function poll() {
      if (inFlight) return;
      inFlight = true;
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
        if (cancelled) return;
        if (response.status === 202) { setError(""); return; }
        const data = (await response.json()) as { error?: string };
        if (cancelled) return;
        if (!response.ok) {
          setError(data.error || "Не удалось подтвердить звонок");
          setStep("form");
          return;
        }
        if (!cancelled) completeLogin();
      } catch {
        if (!cancelled) setError("Не удалось проверить звонок. Проверим ещё раз автоматически. Можно проверить введённый телефон через «Изменить данные».");
      } finally {
        inFlight = false;
      }
    }

    const timer = window.setInterval(() => void poll(), POLL_INTERVAL_MS);
    void poll();
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [step, completeLogin]);

  async function requestCall(targetPhone: string, registrationDraft = registration) {
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
      pollStateRef.current = {
        phone: targetPhone,
        checkId: data.checkId,
        flow,
        registration: registrationDraft,
      };
      setCheckId(data.checkId);
      setCallPhonePretty(data.callPhonePretty);
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

    let registrationDraft = registration;
    if (isRegistration) {
      registrationDraft = {
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
        consent: form.get("consent") === "on",
      };
      setRegistration(registrationDraft);
      if (accountType === "wholesale" && (!registrationDraft.companyName || !registrationDraft.inn)) {
        setError("Выберите организацию из справочника");
        return;
      }
    }
    await requestCall(normalized, registrationDraft);
  }

  return (
    <main className={`auth-page customer-auth customer-auth-centered ${isRegistration ? "customer-auth-register" : ""}`}>
      <StoreHeader />
      <div className="auth-centered-stage">
      <section className={`auth-shell auth-shell-compact ${isRegistration ? "is-register" : "is-login"}`}>
        <div className="auth-intro">
          <div className="auth-intro-copy">
          <h1>
            {step === "waiting"
              ? "Подтвердите номер звонком"
              : isRegistration
                ? "Регистрация"
                : "Вход"}
          </h1>
          {(step === "waiting" || isRegistration) && <p>
            {step === "waiting"
              ? <>Позвоните с номера <span className="auth-confirm-phone">{formatRussianPhoneInput(phone)}</span> на <span className="auth-confirm-phone">{callPhonePretty}</span>. Звонок бесплатный — сервис автоматически его сбросит.</>
              : "Заполните профиль один раз. При следующих заказах данные подставятся автоматически."}
          </p>}</div>
        </div>

        {step === "form" && (
          <form className="auth-form" onSubmit={submit} onChange={event => {
            if (!isRegistration) return;
            const values = new FormData(event.currentTarget);
            setRegistration(current => {
              const next = { ...current, accountType, consent: values.get("consent") === "on" };
              for (const key of ["fullName", "lastName", "patronymic", "email", "deliveryAddress"] as const) {
                if (values.has(key)) next[key] = String(values.get(key) || "");
              }
              return next;
            });
          }}>
            {isRegistration && (
              <>
                <div className="field-grid">
                  <label>
                    Фамилия
                    <input defaultValue={registration.lastName} name="lastName" autoComplete="family-name" required maxLength={80} />
                  </label>
                  <label>
                    Имя
                    <input defaultValue={registration.fullName} name="fullName" autoComplete="given-name" required minLength={2} maxLength={80} />
                  </label>
                </div>
                <label>
                  Отчество
                  <input defaultValue={registration.patronymic} name="patronymic" autoComplete="additional-name" maxLength={80} />
                </label>
              </>
            )}
            <label>
              Телефон
              <input
                ref={phoneInputRef}
                name="phone"
                type="tel"
                defaultValue={phone ? formatRussianPhoneInput(phone) : ""}
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
                  <input defaultValue={registration.email} name="email" type="email" autoComplete="email" required maxLength={254} />
                </label>
                <label>
                  Адрес доставки
                  <input
                    name="deliveryAddress"
                    defaultValue={registration.deliveryAddress}
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
                    <OrganizationLookup initialName={registration.companyName}
                      onClear={() => setRegistration(current => ({ ...current, companyName: "", inn: "", kpp: "", legalAddress: "" }))}
                      onSelect={organization => setRegistration(current => ({ ...current, companyName: organization.name, inn: organization.inn, kpp: organization.kpp, legalAddress: organization.legalAddress }))} />
                    {registration.companyName && <div className="organization-details">
                    <input type="hidden" value={registration.companyName} name="companyName" />
                    <div className="field-grid">
                      <label>
                        ИНН
                        <input value={registration.inn} name="inn" readOnly />
                      </label>
                      <label>
                        КПП
                        <input value={registration.kpp} name="kpp" readOnly placeholder="Из справочника" />
                      </label>
                    </div>
                    <label>
                      Юридический адрес
                      <input value={registration.legalAddress} name="legalAddress" readOnly placeholder="Заполнится из справочника" />
                    </label>
                    </div>}
                  </div>
                )}
                <label className="consent-check">
                  <input type="checkbox" name="consent" required defaultChecked={registration.consent} />
                  <span>
                    Даю <a href="/personal-data-consent" target="_blank" rel="noreferrer">согласие</a> на обработку данных, включая сообщения, фотографии, видео и файлы в чате, по{" "}
                    <a href="/privacy" target="_blank" rel="noreferrer">политике конфиденциальности</a>.
                  </span>
                </label>
              </>
            )}
            {error && <p className="auth-error" role="alert">{error}</p>}
            <button className="primary-button full" disabled={submitting}>
              {submitting
                ? "Запускаем подтверждение…"
                : isRegistration
                  ? "Зарегистрироваться"
                  : "Войти"}
            </button>
            <p className="auth-switch">
              {isRegistration ? (
                <>Уже зарегистрированы? <a href={`/login${switchQuery}`}>Войти</a></>
              ) : (
                <>Нет аккаунта? <a href={`/register${switchQuery}`}>Зарегистрироваться</a></>
              )}
            </p>
          </form>
        )}

        {step === "waiting" && (
          <div className="auth-form">
            <p className="auth-call-number" aria-live="polite">
              <a href={`tel:${callPhonePretty.replace(/[^+0-9]/g, "")}`}>{callPhonePretty}</a>
              <small>Ожидаем звонок и автоматически завершим {isRegistration ? "регистрацию" : "вход"}…</small>
            </p>
            {error && <p className="auth-error" role="alert">{error}</p>}
            <p className="auth-switch">
              <button
                type="button"
                className="text-link"
                disabled={submitting}
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
      </div>
    </main>
  );
}

export function LoginPage() {
  return <AuthPage flow="login" />;
}

export function RegisterPage() {
  return <AuthPage flow="register" />;
}
