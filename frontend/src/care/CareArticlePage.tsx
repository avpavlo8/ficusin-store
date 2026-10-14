import { StoreHeader } from "../StoreHeader";
import NotFoundPage from "../NotFoundPage";
import { careArticles } from "./articles";
import "./care-articles.css";

const sectionIcons: Record<string, string> = {
  recognize: "M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14Zm5-2 5 5",
  lookalikes: "M8 3v18M16 3v18M4 7l4-4 4 4M12 17l4 4 4-4",
  act: "m4 7 2 2 4-4M13 7h7M4 15l2 2 4-4M13 15h7",
  avoid: "M12 3 2 21h20L12 3Zm0 6v5m0 3v1",
  questions: "M9 8a3 3 0 1 1 5 2c-1 1-2 1-2 3m0 3v1M21 11a9 9 0 1 1-4-7",
  watering: "M12 2s-7 8-7 13a7 7 0 0 0 14 0c0-5-7-13-7-13Z",
  followup: "M20 7v5h-5M20 12a8 8 0 1 0-2 6M12 7v5l-3 2",
  leaf: "M5 19C1 8 10 3 21 3c0 11-5 20-16 16Zm0 0L16 8M3 21l2-2",
};
function CareIcon({ kind = "leaf" }: { kind?: string }) {
  const key = kind === "follow-up" ? "followup" : kind;
  return <svg className="care-line-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={sectionIcons[key] || sectionIcons.leaf} /></svg>;
}
const articleImage = (slug: string) => `/assets/care/articles-v1/${slug}.jpg`;

export default function CareArticlePage({ slug }: { slug?: string }) {
  const article = careArticles.find(item => item.slug === slug);
  const readingMinutes = article ? Math.max(1, Math.ceil(article.sections.map(section => [
    section.title, ...(section.paragraphs || []), ...(section.items || []).map(item => `${item.title} ${item.text}`), section.note || "",
  ].join(" ")).join(" ").split(/\s+/).length / 160)) : 0;
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
        <header className="care-story-hero care-story-illustrated">
          <img src={articleImage(article.slug)} alt="" width="1200" height="800" />
          <div><p className="care-kicker">{article.category} · Подробное руководство</p><h1>{article.title}</h1><p className="care-story-deck">{article.description}</p><p className="care-reading-time">{readingMinutes} мин чтения · Можно перейти сразу к нужному разделу</p></div>
        </header>
        <aside className="care-first-action"><CareIcon kind="act" /><div><strong>С чего начать</strong><p>{article.firstAction}</p></div></aside>
        <div className="care-reading-layout">
          <nav className="care-contents" aria-label="В этой статье"><strong>В этой статье</strong>{article.sections.map(section => <a href={`#${section.id}`} key={section.id}>{section.title}</a>)}<a href="#care-help">Нужна помощь?</a></nav>
          <div className="care-story-body">
            {article.sections.map(section => <section className="care-story-section" data-kind={section.id} id={section.id} key={section.id} aria-labelledby={`title-${section.id}`}>
              <h2 id={`title-${section.id}`}><span className="care-section-icon"><CareIcon kind={section.id} /></span>{section.title}</h2>
              {section.paragraphs?.map(text => <p key={text}>{text}</p>)}
              {section.items && (section.ordered ? <ol className="care-action-list">{section.items.map(item => <li key={item.title}><h3>{item.title}</h3><p>{item.text}</p></li>)}</ol> : <ul className="care-observation-list">{section.items.map(item => <li key={item.title}><h3>{item.title}</h3><p>{item.text}</p></li>)}</ul>)}
              {section.note && <aside className="care-story-note"><CareIcon kind="leaf" /><div><strong>Обратите внимание</strong><p>{section.note}</p></div></aside>}
            </section>)}
            <section className="care-story-help" id="care-help"><p className="care-kicker">Разберёмся вместе</p><h2>Не уверены, что с растением?</h2><p>Пришлите фото растения целиком и повреждений крупным планом. Напишите, когда поливали, где стоит горшок и чем уже обрабатывали.</p><a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Написать в Фикусин <span aria-hidden="true">↗</span></a></section>
            <footer className="care-story-sources"><h2>Источники и проверка материала</h2><p>Подготовлено Фикусин · 14 октября 2026. Иллюстрации созданы для Фикусин и показывают тему статьи; определять причину нужно по совокупности признаков. Общие рекомендации сверены с материалами ниже; требования к свету, грунту и поливу уточняйте в паспорте вашего растения.</p><ul>{article.sources.map(source => <li key={source.url}><a href={source.url} target="_blank" rel="noreferrer">{source.title} ↗</a></li>)}</ul></footer>
          </div>
        </div>
      </article> : <header className="care-story-hero"><img src="/assets/care/guide-v4/about.jpg" alt="" width="1200" height="600" /><div><p className="care-kicker">Забота без лишней суеты</p><h1>Помощь растению</h1><p className="care-story-deck">Начните с того, что заметили. Поможем проверить причину и выбрать следующий шаг.</p></div></header>}
      <section className="care-related" aria-labelledby="care-related-title"><h2 id="care-related-title">{article ? "Другие полезные инструкции" : "Что случилось с растением?"}</h2><div>{careArticles.filter(item => item !== article).map(item => <a href={articleLink(item.slug)} key={item.slug}><img className="care-related-image" src={articleImage(item.slug)} alt="" width="1200" height="800" loading="lazy" /><span className="care-kicker">{item.category}</span><h3>{item.title}</h3><p>{item.description}</p><span className="care-read-link">Читать инструкцию <span aria-hidden="true">→</span></span></a>)}</div></section>
      {plantLink && <a className="care-return" href={plantLink}>← К уходу за вашим растением</a>}
    </div>
  </main>;
}
