import { usePageTitle } from "./lib/usePageTitle";
import { StoreHeader } from "./StoreHeader";

export default function NotFoundPage() {
  usePageTitle("Страница не найдена");
  return <main className="not-found-page">
    <StoreHeader />
    <section className="not-found-scene">
      <div>
        <h1>Здесь ничего<br/>не растёт</h1>
        <p>Страница могла переехать. Вернёмся к растениям, с которыми хорошо.</p>
        <div className="not-found-actions"><a className="primary-button" href="/#catalog">В каталог</a><a href="/contacts">Связаться с магазином</a></div>
      </div>
    </section>
  </main>;
}
