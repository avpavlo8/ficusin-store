import { StoreHeader } from "./StoreHeader";

export default function NotFoundPage() {
  return <main className="not-found-page">
    <StoreHeader />
    <section className="not-found-scene ui-card">
      <div>
        <p className="eyebrow">Ошибка 404</p>
        <h1>Страница не найдена</h1>
        <p>Страница могла переехать. Вернёмся к растениям, с которыми хорошо.</p>
        <div className="not-found-actions"><a className="primary-button" href="/#catalog">В каталог</a><a href="/">На главную</a></div>
      </div>
    </section>
  </main>;
}
