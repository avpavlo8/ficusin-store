import Link from "next/link";
import type { ReactNode } from "react";

export function LegalPage({
  eyebrow,
  title,
  children,
}: {
  eyebrow: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <main className="legal-page">
      <header className="legal-header">
        <Link className="brand" href="/">
          <span className="brand-mark">⌇</span>
          <span>Фикусин</span>
        </Link>
        <Link className="account-back" href="/">← Вернуться в магазин</Link>
      </header>
      <article className="legal-document">
        <p className="eyebrow">{eyebrow}</p>
        <h1>{title}</h1>
        <p className="legal-updated">Редакция от 28 июля 2026 года</p>
        {children}
      </article>
      <footer className="legal-footer">
        <span>© 2026 Фикусин</span>
        <Link href="/requisites">Реквизиты</Link>
        <Link href="/offer">Оферта</Link>
        <Link href="/delivery-and-returns">Доставка и возврат</Link>
        <Link href="/privacy">Персональные данные</Link>
      </footer>
    </main>
  );
}
