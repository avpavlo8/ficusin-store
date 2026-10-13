import type { ProductDetail } from "./types";
import "./plant-layout.css";

const topics = [
  ["lighting", "Свет и место"],
  ["watering", "Полив"],
  ["soil", "Грунт"],
  ["repotting", "Пересадка"],
  ["fertilizer", "Подкормка"],
  ["growthRate", "Рост и развитие"],
] as const;

const arrivalSteps = [
  ["Распакуйте", "Аккуратно снимите упаковку, освободите листья и горшок."],
  ["Осмотрите", "Проверьте листья, стебли и влажность грунта."],
  ["Найдите место", "Подберите свет и температуру по рекомендациям ниже."],
  ["Дайте привыкнуть", "Не переставляйте растение без необходимости."],
  ["Оцените пересадку", "Решайте по состоянию корней и субстрата, а не только по дате доставки."],
] as const;

const helpArticles = [
  ["bug", "Мучнистый червец", "Как распознать и что делать", "/care/mealybug"],
  ["mite", "Паутинный клещ", "Первые признаки и план обработки", "/care/spider-mite"],
  ["leaf", "Желтеют листья", "Разбираем возможные причины", "/care/yellow-leaves"],
  ["roots", "Проблемы с корнями", "Как отличить перелив от пересушки", "/care/root-problems"],
] as const;

export function PlantAbout({ product }: { product: ProductDetail }) {
  const paragraphs = (product.description || product.shortDescription || "").split(/\n\s*\n/).filter(Boolean);
  return <article className="plant-about" id="plant-about" aria-labelledby="plant-about-title">
    <header><p className="plant-eyebrow">О растении</p><h2 id="plant-about-title">{product.name}</h2></header>
    <div className="plant-about-copy">
      {paragraphs.map((paragraph, index) => <p key={`${paragraph.slice(0, 18)}-${index}`}>{paragraph}</p>)}
    </div>
  </article>;
}

function HelpIcon({ type }: { type: (typeof helpArticles)[number][0] }) {
  const paths = {
    bug: "M8 9h8v6a4 4 0 0 1-8 0V9Zm4-4v4M7 7l2 2M17 7l-2 2M5 12h3M16 12h3M6 17l3-2M18 17l-3-2",
    mite: "M12 8a5 5 0 1 1 0 10 5 5 0 0 1 0-10Zm0-3v3M6 7l2 2M18 7l-2 2M4 13h3M17 13h3",
    leaf: "M19 5C11 5 6 9 6 15c3 1 7 0 9-2.5S18 8 19 5ZM6 19c2-4 5-7 9-9",
    roots: "M12 4v7M8 7l4 4 4-4M12 11c0 4-4 3-4 8M12 11c0 4 4 3 4 8M12 14v6",
  } as const;
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d={paths[type]}/></svg>;
}

function CareCopy({ text }: { text: string }) {
  return <div className="plant-copy">{text.split(/\n\s*\n/).filter(paragraph => paragraph.trim()).map((paragraph, index) => <p key={index}>{paragraph}</p>)}</div>;
}

export function PlantCareGuide({ product }: { product: ProductDetail }) {
  const passport = product.passport;
  const availableTopics = topics.filter(([key]) => passport[key]?.trim());
  const climate = (["humidity", "temperature"] as const).filter(key => passport[key]?.trim());
  const climateTitle = climate.length === 2 ? "Влажность и температура" : climate[0] === "humidity" ? "Влажность воздуха" : "Температура";
  const climatePanel = climate.length > 0 && <section className="plant-care-panel" data-topic="climate" aria-labelledby="plant-climate-title" key="climate">
    <div className="plant-care-art" aria-hidden="true" />
    <div className="plant-care-text">
      <h3 className="plant-care-heading" id="plant-climate-title">{climateTitle}</h3>
      {climate.map(key => <div className="plant-climate-copy" id={`plant-${key}`} key={key}>
        {climate.length === 2 && <h4>{key === "humidity" ? "Влажность воздуха" : "Температура"}</h4>}
        <CareCopy text={passport[key] || ""} />
      </div>)}
    </div>
  </section>;
  const panels = availableTopics.map(([key, title]) => <section className="plant-care-panel" data-topic={key} id={`plant-${key}`} aria-labelledby={`plant-${key}-title`} key={key}>
    {key !== "growthRate" && <div className="plant-care-art" aria-hidden="true" />}
    <div className="plant-care-text"><h3 className="plant-care-heading" id={`plant-${key}-title`}>{title}</h3><CareCopy text={passport[key] || ""} /></div>
  </section>);
  // Keep climate in reading order even when lighting or watering is unfilled.
  const climateIndex = availableTopics.filter(([key]) => key === "lighting" || key === "watering").length;
  if (climatePanel) panels.splice(climateIndex, 0, climatePanel);

  return <div className="plant-care" id="plant-passport">
    <section className="plant-arrival" aria-labelledby="plant-arrival-title">
      <img className="plant-arrival-photo" src="/assets/care/guide-v2/arrival.jpg" alt="" width="1200" height="800" />
      <div className="plant-arrival-content">
        <h3 id="plant-arrival-title">Первые дни дома</h3>
        <p className="plant-arrival-intro">Помогите растению спокойно освоиться после дороги.</p>
        <ol>{arrivalSteps.map(([title, text], index) => <li key={title}><h4>{title}</h4><p>{text}</p>{index < arrivalSteps.length - 1 && <span className="plant-arrival-arrow" aria-hidden="true">→</span>}</li>)}</ol>
      </div>
    </section>
    {product.importantWarnings.length > 0 && <aside className="plant-warning"><strong>Важно перед началом</strong><ul>{product.importantWarnings.map(warning => <li key={warning}>{warning}</li>)}</ul></aside>}
    <div className="plant-care-panels">{panels}</div>
    {product.careInstructions?.trim() && <aside className="plant-care-note"><h3>На заметку</h3><CareCopy text={product.careInstructions} /></aside>}
    <section className="plant-help" aria-labelledby="plant-help-title"><header><h3 id="plant-help-title">Если что-то не так</h3></header><div>{helpArticles.map(([icon,title,note,url]) => <a href={url} key={url}><i aria-hidden="true"><HelpIcon type={icon}/></i><span><strong>{title}</strong><small>{note}</small></span><b aria-hidden="true">↗</b></a>)}</div></section>
    <aside className="plant-care-contact"><div><h3>Остались вопросы?</h3><p>Поможем подобрать уход именно для вашего растения.</p></div><a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Напишите нам <span aria-hidden="true">→</span></a></aside>
  </div>;
}

export function PlantQuestions({ product }: { product: ProductDetail }) {
  const faq = (product.passport.faq || []).filter(item => item.question.trim() && item.answer.trim());
  return <section className="plant-questions" id="questions" aria-labelledby="plant-questions-title"><p className="plant-eyebrow">Отвечаем на вопросы</p><h2 id="plant-questions-title">Вопросы о растении</h2>{faq.map((item,index) => <details key={`${item.question}-${index}`}><summary>{item.question}</summary><p className="plant-copy">{item.answer}</p></details>)}<a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Задать вопрос ↗</a></section>;
}
