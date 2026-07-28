"use client";

import Link from "next/link";
import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { formatRussianPhoneInput } from "../../lib/phone";

export default function RegisterPage() {
  const router = useRouter();
  const [accountType, setAccountType] = useState<"retail" | "wholesale">("retail");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    const form = new FormData(event.currentTarget);

    try {
      const response = await fetch("/api/auth/register", {
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
      if (!response.ok) throw new Error(data.error || "Не удалось зарегистрироваться");
      router.push("/account");
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось зарегистрироваться");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="auth-page">
      <header className="account-header">
        <Link className="brand" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></Link>
        <Link className="account-back" href="/">← Вернуться в магазин</Link>
      </header>
      <section className="auth-shell">
        <div className="auth-intro">
          <p className="eyebrow">Аккаунт покупателя</p>
          <h1>Регистрация</h1>
          <p>Сохраняйте историю заказов и получайте персональную скидку. Оптовые аккаунты становятся активными после проверки реквизитов.</p>
        </div>
        <form className="auth-form" onSubmit={submit}>
          <fieldset>
            <legend>Тип покупателя</legend>
            <div className="account-type-options">
              <label className={accountType === "retail" ? "selected" : ""}>
                <input type="radio" name="accountType" checked={accountType === "retail"} onChange={() => setAccountType("retail")} />
                <span><b>Розничный</b><small>Заказ от 1 штуки и накопительная скидка</small></span>
              </label>
              <label className={accountType === "wholesale" ? "selected" : ""}>
                <input type="radio" name="accountType" checked={accountType === "wholesale"} onChange={() => setAccountType("wholesale")} />
                <span><b>Оптовый</b><small>Минимальное количество по товару и оплата по счёту</small></span>
              </label>
            </div>
          </fieldset>
          <fieldset>
            <legend>Контактные данные</legend>
            <label>Имя и фамилия<input name="fullName" autoComplete="name" required minLength={2} maxLength={120} /></label>
            <div className="field-grid">
              <label>Телефон<input name="phone" autoComplete="tel" inputMode="tel" required maxLength={18} placeholder="+7 900 000-00-00" onInput={(event) => { event.currentTarget.value = formatRussianPhoneInput(event.currentTarget.value); }} /></label>
              <label>Email<input name="email" type="email" autoComplete="email" required /></label>
            </div>
            <label>Пароль<input name="password" type="password" autoComplete="new-password" required minLength={10} maxLength={128} /><small>Не менее 10 символов, минимум одна буква и одна цифра</small></label>
          </fieldset>
          {accountType === "wholesale" && (
            <fieldset>
              <legend>Реквизиты организации</legend>
              <label>Название ИП или организации<input name="companyName" required maxLength={180} /></label>
              <div className="field-grid">
                <label>ИНН<input name="inn" inputMode="numeric" required pattern="\d{10}|\d{12}" maxLength={12} /></label>
                <label>КПП, если есть<input name="kpp" inputMode="numeric" pattern="\d{9}" maxLength={9} /></label>
              </div>
              <label>Юридический адрес<input name="legalAddress" maxLength={300} /></label>
              <p className="wholesale-note">После регистрации заявка получит статус «На проверке». До подтверждения покупки будут доступны по розничным условиям.</p>
            </fieldset>
          )}
          <label className="consent-check">
            <input name="consent" type="checkbox" required />
            <span>Я принимаю <Link href="/offer" target="_blank">оферту</Link> и даю согласие на обработку данных по <Link href="/privacy" target="_blank">политике конфиденциальности</Link>.</span>
          </label>
          {error && <p className="auth-error" role="alert">{error}</p>}
          <button className="primary-button full" disabled={submitting}>{submitting ? "Создаём аккаунт…" : "Зарегистрироваться"}</button>
          <p className="auth-switch">Уже есть аккаунт? <Link href="/login">Войти</Link></p>
        </form>
      </section>
    </main>
  );
}
