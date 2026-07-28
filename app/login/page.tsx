"use client";

import Link from "next/link";
import { FormEvent, Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { formatRussianPhoneInput } from "../../lib/phone";

function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    const form = new FormData(event.currentTarget);
    try {
      const response = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          identifier: form.get("identifier"),
          password: form.get("password"),
        }),
      });
      const data = (await response.json()) as { error?: string };
      if (!response.ok) throw new Error(data.error || "Не удалось войти");
      const returnTo = searchParams.get("returnTo");
      router.push(returnTo?.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/account");
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось войти");
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
      <section className="auth-shell auth-shell-compact">
        <div className="auth-intro">
          <p className="eyebrow">Личный кабинет</p>
          <h1>Вход</h1>
          <p>Введите email или российский номер телефона и пароль.</p>
        </div>
        <form className="auth-form" onSubmit={submit}>
          <label>Телефон или email<input name="identifier" autoComplete="username" required onInput={(event) => { if (!event.currentTarget.value.includes("@") && /^[+\d\s()-]*$/.test(event.currentTarget.value)) event.currentTarget.value = formatRussianPhoneInput(event.currentTarget.value); }} /></label>
          <label>Пароль<input name="password" type="password" autoComplete="current-password" required /></label>
          {error && <p className="auth-error" role="alert">{error}</p>}
          <button className="primary-button full" disabled={submitting}>{submitting ? "Входим…" : "Войти"}</button>
          <p className="auth-switch">Нет аккаунта? <Link href="/register">Зарегистрироваться</Link></p>
        </form>
      </section>
    </main>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={<main className="auth-page" />}>
      <LoginForm />
    </Suspense>
  );
}
