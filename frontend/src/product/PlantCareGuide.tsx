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
  return <article className="plant-about" id="plant-about" aria-labelledby="plant-about-title">
    <header><p className="plant-eyebrow">Знакомство с растением</p><h2 id="plant-about-title">История и особенности<br/>{product.name}</h2></header>
    <div className="plant-about-copy">
      <p className={product.description ? "plant-copy" : "plant-empty"}>{product.description || "Подробный рассказ о растении готовится."}</p>
      <p className="plant-about-note">В этом разделе будет подробный редакционный материал: происхождение растения, особенности сорта, характер роста и то, за что его выбирают. Короткое описание для решения о покупке остаётся наверху карточки.</p>
    </div>
  </article>;
}

export function PlantCareGuide({ product }: { product: ProductDetail }) {
  const passport = product.passport;
  const facts = [["☀", "Свет", product.lightLevel || passport.lighting], ["◌", "Полив", product.watering || passport.watering], ["°", "Температура", passport.temperature]];
  return <div className="plant-care" id="plant-passport">
    <header className="plant-care-heading" id="care-guide"><div><p className="plant-eyebrow">Инструкция Фикусина</p><h2>Всё, чтобы растению<br/><em>было хорошо.</em></h2></div><p>{product.name}<span>От первых дней дома до регулярного ухода</span></p></header>
    <div className="plant-essentials">{facts.map(([icon,label,value]) => <div key={label}><i aria-hidden="true">{icon}</i><div><span>{label}</span><strong>{value ? attributeValue(value) : "Уточняем рекомендации"}</strong></div></div>)}</div>
    {product.importantWarnings.length > 0 && <aside className="plant-warning"><strong>Важно перед началом</strong><ul>{product.importantWarnings.map(warning => <li key={warning}>{warning}</li>)}</ul></aside>}
    <section className="plant-arrival" aria-labelledby="plant-arrival-title"><header><p className="plant-eyebrow">Первое знакомство с домом</p><h3 id="plant-arrival-title">Растение приехало.<br/>Что дальше?</h3></header><ol><li><span>01</span><div><h4>Осмотрите</h4><p>Снимите упаковку, проверьте листья и грунт.</p></div></li><li><span>02</span><div><h4>Дайте привыкнуть</h4><p>Выберите постоянное место без сквозняка.</p></div></li><li><span>03</span><div><h4>Не спешите</h4><p>Сначала проверьте грунт, затем решайте, нужен ли полив.</p></div></li></ol></section>
    <section className="plant-routine" aria-labelledby="plant-routine-title"><div className="plant-routine-intro"><p className="plant-eyebrow">Шаг за шагом</p><h3 id="plant-routine-title">Как ухаживать</h3><p>Выберите тему — всё необходимое собрано внутри.</p>{product.careInstructions && <p className="plant-copy">{product.careInstructions}</p>}</div><div className="plant-topics">{topics.map(([key,title,hint,image],index) => <details key={key} open={index === 0} id={`plant-${key}`}><summary><span className="plant-topic-number">{String(index+1).padStart(2,"0")}</span><span>{title}<small>{hint}</small></span><span className="plant-topic-toggle" aria-hidden="true"/></summary><div className={`plant-topic-content${image ? " has-image" : ""}`}>{image && <img src={image} alt="" loading="lazy" width="480" height="360"/>}<p className={passport[key]?.trim() ? "plant-copy" : "plant-empty"}>{passport[key]?.trim() || pending}</p></div></details>)}</div></section>
    <section className="plant-help" aria-labelledby="plant-help-title"><header><p className="plant-eyebrow">База знаний</p><h3 id="plant-help-title">Если что-то не так</h3><p>Подробные инструкции по отдельным проблемам вынесены в статьи.</p></header><div>{helpArticles.map(([title,note,url]) => <a href={url} key={url}><span><strong>{title}</strong><small>{note}</small></span><i aria-hidden="true">↗</i></a>)}</div></section>
    {(passport.toxicity || product.petSafety) && <aside className="plant-safety"><p className="plant-eyebrow">Дети и питомцы</p><p>{passport.toxicity || attributeValue(product.petSafety || "")}</p></aside>}
  </div>;
}

export function PlantQuestions({ product }: { product: ProductDetail }) {
  const faq = (product.passport.faq || []).filter(item => item.question.trim() && item.answer.trim());
  return <section className="plant-questions" id="questions" aria-labelledby="plant-questions-title"><p className="plant-eyebrow">Отвечаем на вопросы</p><h2 id="plant-questions-title">Вопросы о растении</h2>{faq.length ? faq.map((item,index) => <details key={`${item.question}-${index}`}><summary>{item.question}</summary><p className="plant-copy">{item.answer}</p></details>) : <p className="plant-empty">Здесь появятся ответы на частые вопросы об этом растении.</p>}<a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Задать вопрос ↗</a></section>;
}
