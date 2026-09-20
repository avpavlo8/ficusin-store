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
  const paths: Record<(typeof topics)[number][0], string> = {
    lighting: "M12 5v2M12 17v2M5 12h2M17 12h2M7 7l1.4 1.4M15.6 15.6 17 17M17 7l-1.4 1.4M8.4 15.6 7 17",
    watering: "M12 4S7 9.8 7 14a5 5 0 0 0 10 0c0-4.2-5-10-5-10Z",
    humidity: "M8 6S5 9.4 5 12a3 3 0 0 0 6 0C11 9.4 8 6 8 6Zm8 5s-3 3.4-3 6a3 3 0 0 0 6 0c0-2.6-3-6-3-6Z",
    temperature: "M14 14.4V7a3 3 0 0 0-6 0v7.4a5 5 0 1 0 6 0ZM11 10v7",
    soil: "M4 9h16M6 9l2 10h8l2-10M9 5c1.2.2 2.2 1 3 2.2C12.8 6 13.8 5.2 15 5",
    repotting: "M5 10h14l-2 9H7l-2-9Zm7-6v6M9 7l3-3 3 3",
    fertilizer: "M8 5h8v3l2 3v8H6v-8l2-3V5Zm1 9h6M12 11v6",
    growthRate: "M5 18V8M5 18h14M8 15l3-4 3 2 4-6M15 7h3v3",
  };
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d={paths[type]}/></svg>;
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
