import { StoreHeader } from "../StoreHeader";
import NotFoundPage from "../NotFoundPage";
import { careArticles } from "./articles";
import "./care-articles.css";

export default function CareArticlePage({ slug }: { slug?: string }) {
  const article = careArticles.find(item => item.slug === slug);
  const plant = new URLSearchParams(window.location.search).get("plant");
  const plantLink = plant ? `/product/${encodeURIComponent(plant)}#plant-passport` : undefined;
  const articleLink = (value: string) => `/care/${value}${plant ? `?plant=${encodeURIComponent(plant)}` : ""}`;
  if (slug && !article) return <NotFoundPage />;

  return <main className="care-reading-page">
    <StoreHeader />
    <div className="care-reading-shell">
      <nav className="care-breadcrumbs" aria-label="Хлебные крошки">
        <a href="/">Главная</a><span aria-hidden="true">/</span>
        {article ? <><a href={`/care${plant ? `?plant=${encodeURIComponent(plant)}` : ""}`}>Помощь растению</a><span aria-hidden="true">/</span><span aria-current="page">{article.title}</span></> : <span aria-current="page">Помощь растению</span>}
      </nav>
      {plantLink && <a className="care-return" href={plantLink}>← К уходу за вашим растением</a>}
      {article ? <article>
        <header className="care-story-hero">
          <img src={`/assets/care/guide-v4/${article.image}.jpg`} alt="" width="1200" height="600" />
          <div><p className="care-kicker">{article.category} · Памятка Фикусин</p><h1>{article.title}</h1><p className="care-story-deck">{article.description}</p></div>
        </header>
        <aside className="care-first-action"><strong>С чего начать</strong><p>{article.firstAction}</p></aside>
        <div className="care-reading-layout">
          <nav className="care-contents" aria-label="В этой статье"><strong>В этой статье</strong>{article.sections.map(section => <a href={`#${section.id}`} key={section.id}>{section.title}</a>)}<a href="#care-help">Нужна помощь?</a></nav>
          <div className="care-story-body">
            {article.sections.map(section => <section className="care-story-section" id={section.id} key={section.id} aria-labelledby={`title-${section.id}`}>
              <h2 id={`title-${section.id}`}>{section.title}</h2>
              {section.paragraphs?.map(text => <p key={text}>{text}</p>)}
              {section.items && (section.ordered ? <ol className="care-action-list">{section.items.map(item => <li key={item.title}><h3>{item.title}</h3><p>{item.text}</p></li>)}</ol> : <ul className="care-observation-list">{section.items.map(item => <li key={item.title}><h3>{item.title}</h3><p>{item.text}</p></li>)}</ul>)}
              {section.note && <aside className="care-story-note">{section.note}</aside>}
            </section>)}
            <section className="care-story-help" id="care-help"><p className="care-kicker">Разберёмся вместе</p><h2>Не уверены, что с растением?</h2><p>Пришлите фото растения целиком и повреждений крупным планом. Напишите, когда поливали, где стоит горшок и чем уже обрабатывали.</p><a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Написать в Фикусин <span aria-hidden="true">↗</span></a></section>
            <footer className="care-story-sources"><h2>Источники и проверка материала</h2><p>Подготовлено Фикусин · 14 октября 2026. Общие рекомендации сверены с материалами ниже; требования к свету, грунту и поливу уточняйте в паспорте вашего растения.</p><ul>{article.sources.map(source => <li key={source.url}><a href={source.url} target="_blank" rel="noreferrer">{source.title} ↗</a></li>)}</ul></footer>
          </div>
        </div>
      </article> : <header className="care-story-hero"><img src="/assets/care/guide-v4/about.jpg" alt="" width="1200" height="600" /><div><p className="care-kicker">Забота без лишней суеты</p><h1>Помощь растению</h1><p className="care-story-deck">Начните с того, что заметили. Поможем проверить причину и выбрать следующий шаг.</p></div></header>}
      <section className="care-related" aria-labelledby="care-related-title"><h2 id="care-related-title">{article ? "Другие полезные инструкции" : "Что случилось с растением?"}</h2><div>{careArticles.filter(item => item !== article).map(item => <a href={articleLink(item.slug)} key={item.slug}><span className="care-kicker">{item.category}</span><h3>{item.title}</h3><p>{item.description}</p><span className="care-read-link">Читать инструкцию <span aria-hidden="true">→</span></span></a>)}</div></section>
      {plantLink && <a className="care-return" href={plantLink}>← К уходу за вашим растением</a>}
    </div>
  </main>;
}
