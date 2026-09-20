import { useState } from "react";
import type { ProductDetail } from "./types";
import "./plant-layout.css";

const topics = [
  ["lighting", "Свет и место", "Куда поставить растение", "/assets/care/topics/light.jpg"],
  ["watering", "Полив", "Когда и как поливать", "/assets/care/topics/watering.jpg"],
  ["humidity", "Влажность воздуха", "Нужен ли дополнительный уход", "/assets/care/topics/humidity.jpg"],
  ["temperature", "Температура", "Комфортные условия дома", "/assets/care/topics/temperature.jpg"],
  ["soil", "Грунт", "Подходящий состав", "/assets/care/topics/soil.jpg"],
  ["repotting", "Пересадка", "Когда и во что пересаживать", "/assets/care/topics/repotting.jpg"],
  ["fertilizer", "Подкормка", "Как поддержать рост", "/assets/care/topics/fertilizer.jpg"],
  ["growthRate", "Рост и развитие", "Чего ожидать со временем", "/assets/care/topics/growth.jpg"],
] as const;

const helpArticles = [
  ["bug", "Мучнистый червец", "Как распознать и что делать", "/care/mealybug"],
  ["mite", "Паутинный клещ", "Первые признаки и план обработки", "/care/spider-mite"],
  ["leaf", "Желтеют листья", "Разбираем возможные причины", "/care/yellow-leaves"],
  ["roots", "Проблемы с корнями", "Как отличить перелив от пересушки", "/care/root-problems"],
] as const;

const pending = "Информация для этого растения готовится.";

export function PlantAbout({ product }: { product: ProductDetail }) {
  const paragraphs = (product.description || "Подробный рассказ о растении готовится.").split(/\n\s*\n/).filter(Boolean);
  return <article className="plant-about" id="plant-about" aria-labelledby="plant-about-title">
    <header><p className="plant-eyebrow">О растении</p><h2 id="plant-about-title">{product.name}</h2></header>
    <div className={`plant-about-copy${product.description ? "" : " plant-empty"}`}>
      {paragraphs.map((paragraph, index) => <p key={`${paragraph.slice(0, 18)}-${index}`}>{paragraph}</p>)}
    </div>
  </article>;
}

function TopicIcon({ type }: { type: (typeof topics)[number][0] }) {
  if (type === "lighting") return <svg viewBox="0 0 32 32" aria-hidden="true"><circle className="icon-sun" cx="16" cy="16" r="6"/><path className="icon-sun-ray" d="M16 3v5M16 24v5M3 16h5M24 16h5M6.8 6.8l3.5 3.5M21.7 21.7l3.5 3.5M25.2 6.8l-3.5 3.5M10.3 21.7l-3.5 3.5"/></svg>;
  if (type === "watering") return <svg viewBox="0 0 32 32" aria-hidden="true"><path className="icon-watercan" d="M8 13h13v12H8z"/><path className="icon-watercan-dark" d="M21 16c4-5 6-5 8-4-1 4-4 7-8 8M8 16H4v6h4M12 13c0-5 7-5 7 0"/><path className="icon-waterdrop" d="M27 21s-2 2.5-2 4a2 2 0 0 0 4 0c0-1.5-2-4-2-4Z"/></svg>;
  if (type === "humidity") return <svg viewBox="0 0 32 32" aria-hidden="true"><path className="icon-mist" d="M5 11h17M9 16h18M4 21h16"/><path className="icon-waterdrop" d="M25 5s-3 3.5-3 5.5a3 3 0 0 0 6 0C28 8.5 25 5 25 5Z"/></svg>;
  if (type === "temperature") return <svg viewBox="0 0 32 32" aria-hidden="true"><path className="icon-thermometer" d="M19 18V8a5 5 0 0 0-10 0v10a8 8 0 1 0 10 0Z"/><path className="icon-heat" d="M14 10v13"/></svg>;
  if (type === "soil") return <svg viewBox="0 0 32 32" aria-hidden="true"><path className="icon-pot" d="M6 12h20l-3 15H9L6 12Z"/><path className="icon-soil" d="M7 12c4-3 14-3 18 0"/><path className="icon-leaf" d="M16 11c-1-5 3-7 7-6-1 4-3 6-7 6Zm0 0c-1-4-4-5-7-4 1 3 3 5 7 4Z"/></svg>;
  if (type === "repotting") return <svg viewBox="0 0 32 32" aria-hidden="true"><path className="icon-pot" d="M7 17h18l-3 11H10L7 17Z"/><path className="icon-leaf" d="M16 17V7m0 4c-4 0-6-2-7-6 4 0 7 2 7 6Zm0 2c4 0 6-2 7-6-4 0-7 2-7 6Z"/><path className="icon-arrow" d="M4 11h6M7 8l3 3-3 3"/></svg>;
  if (type === "fertilizer") return <svg viewBox="0 0 32 32" aria-hidden="true"><path className="icon-bottle" d="M11 5h10v5l3 4v13H8V14l3-4V5Z"/><path className="icon-label" d="M11 16h10v7H11z"/><path className="icon-leaf" d="M16 21c-1-3 1-5 4-5 0 3-1 5-4 5Z"/></svg>;
  return <svg viewBox="0 0 32 32" aria-hidden="true"><path className="icon-growth-pot" d="M9 23h14l-2 6H11l-2-6Z"/><path className="icon-stem" d="M16 23V8"/><path className="icon-leaf" d="M16 14c-5 0-7-3-7-7 5 0 7 3 7 7Zm0 4c5 0 7-3 7-7-5 0-7 3-7 7Z"/><path className="icon-arrow" d="M23 9V3m0 0-3 3m3-3 3 3"/></svg>;
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

export function PlantCareGuide({ product }: { product: ProductDetail }) {
  const passport = product.passport;
  const [activeTopic, setActiveTopic] = useState<(typeof topics)[number][0]>(topics[0][0]);
  const selectedTopic = topics.find(([key]) => key === activeTopic) || topics[0];
  return <div className="plant-care" id="plant-passport">
    <section className="plant-arrival" aria-labelledby="plant-arrival-title"><header><div><p className="plant-eyebrow">Сразу после доставки</p><h3 id="plant-arrival-title">Первые дни дома</h3></div><p>Универсальная памятка: пять спокойных шагов помогают растению освоиться после дороги.</p></header><div className="plant-arrival-steps"><div><img src="/assets/care/first-days-home.jpg" alt="Пять первых действий: распаковать, осмотреть, выбрать место, дать привыкнуть и при необходимости пересадить" width="2172" height="724"/><ol><li><h4>Распакуйте</h4><p>Аккуратно снимите транспортировочную упаковку.</p></li><li><h4>Осмотрите</h4><p>Проверьте листья, стебли и влажность грунта.</p></li><li><h4>Найдите место</h4><p>Без сквозняка, батареи и резкого прямого солнца.</p></li><li><h4>Дайте привыкнуть</h4><p>Не тревожьте растение в первые дни дома.</p></li><li><h4>Пересадите при необходимости</h4><p>Обычно не раньше чем через 14 дней.</p></li></ol></div></div></section>
    {product.importantWarnings.length > 0 && <aside className="plant-warning"><strong>Важно перед началом</strong><ul>{product.importantWarnings.map(warning => <li key={warning}>{warning}</li>)}</ul></aside>}
    <section className="plant-routine" aria-labelledby="plant-routine-title"><header className="plant-routine-intro"><div><p className="plant-eyebrow">Подробный уход</p><h3 id="plant-routine-title">Всё по темам</h3></div><p>Выберите нужный вопрос — инструкция откроется ниже.</p></header><div className="plant-topic-tabs" role="tablist" aria-label="Темы ухода">{topics.map(([key,title]) => <button key={key} type="button" role="tab" aria-selected={activeTopic === key} aria-controls={`plant-${key}`} id={`plant-${key}-tab`} onClick={() => setActiveTopic(key)}><span><TopicIcon type={key}/></span><strong>{title}</strong></button>)}</div><article className={`plant-topic-content${selectedTopic[3] ? " has-image" : ""}`} id={`plant-${selectedTopic[0]}`} role="tabpanel" aria-labelledby={`plant-${selectedTopic[0]}-tab`}>{selectedTopic[3] && <img src={selectedTopic[3]} alt="" loading="lazy" width="1536" height="1152"/>}<div><p className="plant-topic-kicker">{selectedTopic[2]}</p><h4>{selectedTopic[1]}</h4><p className={passport[selectedTopic[0]]?.trim() ? "plant-copy" : "plant-empty"}>{passport[selectedTopic[0]]?.trim() || pending}</p>{product.careInstructions && selectedTopic[0] === "lighting" && <p className="plant-copy plant-extra-copy">{product.careInstructions}</p>}</div></article></section>
    <section className="plant-help" aria-labelledby="plant-help-title"><header><p className="plant-eyebrow">База знаний</p><h3 id="plant-help-title">Если что-то не так</h3><p>Подробные инструкции по отдельным проблемам вынесены в статьи.</p></header><div>{helpArticles.map(([icon,title,note,url]) => <a href={url} key={url}><i aria-hidden="true"><HelpIcon type={icon}/></i><span><strong>{title}</strong><small>{note}</small></span><b aria-hidden="true">↗</b></a>)}</div></section>
  </div>;
}

export function PlantQuestions({ product }: { product: ProductDetail }) {
  const faq = (product.passport.faq || []).filter(item => item.question.trim() && item.answer.trim());
  return <section className="plant-questions" id="questions" aria-labelledby="plant-questions-title"><p className="plant-eyebrow">Отвечаем на вопросы</p><h2 id="plant-questions-title">Вопросы о растении</h2>{faq.length ? faq.map((item,index) => <details key={`${item.question}-${index}`}><summary>{item.question}</summary><p className="plant-copy">{item.answer}</p></details>) : <p className="plant-empty">Здесь появятся ответы на частые вопросы об этом растении.</p>}<a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Задать вопрос ↗</a></section>;
}
