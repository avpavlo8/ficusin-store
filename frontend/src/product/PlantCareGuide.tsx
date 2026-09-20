import { useState } from "react";
import type { ProductDetail } from "./types";
import { attributeValue } from "./types";
import "./plant-layout.css";

const topics = [
  ["lighting", "Свет и место", "Куда поставить растение", "/images/care/light.webp"],
  ["watering", "Полив", "Когда и как поливать", "/images/care/watering.webp"],
  ["humidity", "Влажность воздуха", "Нужен ли дополнительный уход", "/images/care/humidity.webp"],
  ["temperature", "Температура", "Комфортные условия дома", ""],
  ["soil", "Грунт", "Подходящий состав", ""],
  ["repotting", "Пересадка", "Когда и во что пересаживать", "/images/care/repotting.webp"],
  ["fertilizer", "Подкормка", "Как поддержать рост", ""],
  ["growthRate", "Рост и развитие", "Чего ожидать со временем", ""],
] as const;

const helpArticles = [
  ["Мучнистый червец", "Как распознать и что делать", "/care/mealybug"],
  ["Паутинный клещ", "Первые признаки и план обработки", "/care/spider-mite"],
  ["Желтеют листья", "Разбираем возможные причины", "/care/yellow-leaves"],
  ["Проблемы с корнями", "Как отличить перелив от пересушки", "/care/root-problems"],
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

function CareIcon({ type }: { type: "light" | "water" | "temperature" }) {
  if (type === "light") return <svg viewBox="0 0 32 32" aria-hidden="true"><circle cx="16" cy="16" r="5"/><path d="M16 2v5M16 25v5M2 16h5M25 16h5M6.1 6.1l3.5 3.5M22.4 22.4l3.5 3.5M25.9 6.1l-3.5 3.5M9.6 22.4l-3.5 3.5"/></svg>;
  if (type === "water") return <svg viewBox="0 0 32 32" aria-hidden="true"><path d="M16 3S7 13.1 7 20a9 9 0 0 0 18 0C25 13.1 16 3 16 3Z"/><path d="M12 21.5c.7 2.2 2.1 3.3 4.3 3.5"/></svg>;
  return <svg viewBox="0 0 32 32" aria-hidden="true"><path d="M18.5 18.1V7.5a4 4 0 0 0-8 0v10.6a7 7 0 1 0 8 0Z"/><path d="M14.5 11v11"/></svg>;
}

export function PlantCareGuide({ product }: { product: ProductDetail }) {
  const passport = product.passport;
  const [activeTopic, setActiveTopic] = useState<(typeof topics)[number][0]>(topics[0][0]);
  const facts = [["light", "Свет", product.lightLevel || passport.lighting], ["water", "Полив", product.watering || passport.watering], ["temperature", "Температура", passport.temperature]] as const;
  const selectedTopic = topics.find(([key]) => key === activeTopic) || topics[0];
  return <div className="plant-care" id="plant-passport">
    <header className="plant-care-heading" id="care-guide"><div><p className="plant-eyebrow">Инструкция Фикусина</p><h2>Всё, чтобы растению<br/><em>было хорошо.</em></h2></div><p>{product.name}<span>От первых дней дома до регулярного ухода</span></p></header>
    <div className="plant-essentials">{facts.map(([icon,label,value]) => <div key={label}><i><CareIcon type={icon}/></i><div><span>{label}</span><strong>{value ? attributeValue(value) : "Уточняем рекомендации"}</strong></div></div>)}</div>
    {product.importantWarnings.length > 0 && <aside className="plant-warning"><strong>Важно перед началом</strong><ul>{product.importantWarnings.map(warning => <li key={warning}>{warning}</li>)}</ul></aside>}
    <section className="plant-arrival" aria-labelledby="plant-arrival-title"><header><div><p className="plant-eyebrow">Сразу после доставки</p><h3 id="plant-arrival-title">Первые дни дома</h3></div><p>Одна памятка для любого растения: спокойно распакуйте, осмотрите и дайте ему привыкнуть к новому месту.</p></header><img src="/assets/care/first-days-home.png" alt="Последовательность первых действий: распаковать растение, осмотреть листья, поставить у окна и дать привыкнуть" width="1942" height="809"/><ol><li><span>01</span><div><h4>Распакуйте и осмотрите</h4><p>Проверьте листья, упаковку и влажность грунта.</p></div></li><li><span>02</span><div><h4>Найдите спокойное место</h4><p>Без сквозняка, батареи и резкого прямого солнца.</p></div></li><li><span>03</span><div><h4>Дайте время привыкнуть</h4><p>Не пересаживайте сразу и поливайте только после проверки грунта.</p></div></li></ol></section>
    <section className="plant-routine" aria-labelledby="plant-routine-title"><header className="plant-routine-intro"><div><p className="plant-eyebrow">Подробный уход</p><h3 id="plant-routine-title">Всё по темам</h3></div><p>Выберите нужный вопрос — инструкция откроется ниже.</p></header><div className="plant-topic-tabs" role="tablist" aria-label="Темы ухода">{topics.map(([key,title],index) => <button key={key} type="button" role="tab" aria-selected={activeTopic === key} aria-controls={`plant-${key}`} id={`plant-${key}-tab`} onClick={() => setActiveTopic(key)}><span>{String(index+1).padStart(2,"0")}</span>{title}</button>)}</div><article className={`plant-topic-content${selectedTopic[3] ? " has-image" : ""}`} id={`plant-${selectedTopic[0]}`} role="tabpanel" aria-labelledby={`plant-${selectedTopic[0]}-tab`}>{selectedTopic[3] && <img src={selectedTopic[3]} alt="" loading="lazy" width="480" height="360"/>}<div><p className="plant-topic-kicker">{selectedTopic[2]}</p><h4>{selectedTopic[1]}</h4><p className={passport[selectedTopic[0]]?.trim() ? "plant-copy" : "plant-empty"}>{passport[selectedTopic[0]]?.trim() || pending}</p>{product.careInstructions && selectedTopic[0] === "lighting" && <p className="plant-copy plant-extra-copy">{product.careInstructions}</p>}</div></article></section>
    <section className="plant-help" aria-labelledby="plant-help-title"><header><p className="plant-eyebrow">База знаний</p><h3 id="plant-help-title">Если что-то не так</h3><p>Подробные инструкции по отдельным проблемам вынесены в статьи.</p></header><div>{helpArticles.map(([title,note,url]) => <a href={url} key={url}><span><strong>{title}</strong><small>{note}</small></span><i aria-hidden="true">↗</i></a>)}</div></section>
    {(passport.toxicity || product.petSafety) && <aside className="plant-safety"><p className="plant-eyebrow">Дети и питомцы</p><p>{passport.toxicity || attributeValue(product.petSafety || "")}</p></aside>}
  </div>;
}

export function PlantQuestions({ product }: { product: ProductDetail }) {
  const faq = (product.passport.faq || []).filter(item => item.question.trim() && item.answer.trim());
  return <section className="plant-questions" id="questions" aria-labelledby="plant-questions-title"><p className="plant-eyebrow">Отвечаем на вопросы</p><h2 id="plant-questions-title">Вопросы о растении</h2>{faq.length ? faq.map((item,index) => <details key={`${item.question}-${index}`}><summary>{item.question}</summary><p className="plant-copy">{item.answer}</p></details>) : <p className="plant-empty">Здесь появятся ответы на частые вопросы об этом растении.</p>}<a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Задать вопрос ↗</a></section>;
}
