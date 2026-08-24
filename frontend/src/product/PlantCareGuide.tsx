import { useState } from "react";
import type { PlantPassportData, ProductDetail } from "./types";

type CareTopic = "lighting" | "watering" | "humidity" | "repotting";

const quickFacts = [
  ["☀", "Освещение", "lighting"], ["◌", "Полив", "watering"],
  ["≈", "Влажность", "humidity"], ["°", "Температура", "temperature"],
] as const;

const routine = [["soil", "Грунт"], ["fertilizer", "Подкормка"], ["repotting", "Пересадка"], ["growthRate", "Рост"]] as const;

const visualCare: Array<{ key: CareTopic; title: string; image: string; hint: string }> = [
  { key: "lighting", title: "Свет", image: "/images/care/light.webp", hint: "Куда поставить" },
  { key: "watering", title: "Полив", image: "/images/care/watering.webp", hint: "Когда пора поливать" },
  { key: "humidity", title: "Влажность", image: "/images/care/humidity.webp", hint: "Нужны ли опрыскивания" },
  { key: "repotting", title: "Пересадка", image: "/images/care/repotting.webp", hint: "Как выбрать момент" },
];

const fallback: Record<CareTopic, string> = {
  lighting: "Поставьте растение в светлое место без жёсткого полуденного солнца. Вытягивание обычно говорит о нехватке света, светлые сухие пятна — о его избытке.",
  watering: "Не поливайте по календарю. Проверьте грунт пальцем и поливайте только после подсыхания верхнего слоя. Лишнюю воду из поддона слейте.",
  humidity: "Держите растение вдали от горячих батарей и холодных сквозняков. Опрыскивание не заменяет полив и подходит не всем видам.",
  repotting: "Не спешите пересаживать сразу после доставки. Сначала дайте растению адаптироваться; срочная пересадка нужна только при проблемах с грунтом или корнями.",
};

function topicText(passport: PlantPassportData, key: CareTopic) {
  return passport[key]?.trim() || fallback[key];
}

export function PlantCareGuide({ product }: { product: ProductDetail }) {
  const passport = product.passport || {};
  const [activeTopic, setActiveTopic] = useState<CareTopic>("lighting");
  const selected = visualCare.find((item) => item.key === activeTopic) || visualCare[0];
  const facts = quickFacts.filter(([, , key]) => passport[key]);
  const steps = routine.filter(([key]) => passport[key]);
  const faq = (passport.faq || []).filter((item) => item.question.trim() && item.answer.trim());
  return <section className="care-guide pdp-info-card" id="care-guide">
    <header className="care-guide-heading"><div><p>Инструкция Фикусина</p><h2>Как ухаживать за {product.name}</h2></div><span>Всё важное — от распаковки до регулярного ухода</span></header>
    <section className="care-arrival" aria-labelledby="care-arrival-title"><div className="care-section-number">01</div><div><p className="care-kicker">Сразу после покупки</p><h3 id="care-arrival-title">Первые дни дома</h3></div><ol><li><strong>Осмотрите</strong><span>Снимите упаковку, проверьте листья и грунт. Изолируйте от других растений при признаках вредителей.</span></li><li><strong>Дайте привыкнуть</strong><span>Выберите постоянное место без сквозняка и не переставляйте растение каждый день.</span></li><li><strong>Не спешите</strong><span>Поливайте только после проверки грунта. Пересадку и подкормку отложите на период адаптации.</span></li></ol></section>
    {facts.length > 0 && <div className="care-quick-facts">{facts.map(([icon,label,key])=><article key={key}><i aria-hidden="true">{icon}</i><small>{label}</small><strong>{passport[key]}</strong></article>)}</div>}
    {product.importantWarnings.length > 0 && <aside className="care-warning"><strong>Важно знать</strong><ul>{product.importantWarnings.map((warning)=><li key={warning}>{warning}</li>)}</ul></aside>}
    <div className="care-guide-body">
      <article className="care-intro"><span>02</span><div><h3>Знакомство с растением</h3><p>{product.description || product.shortDescription || "Описание растения готовится."}</p></div></article>
      <article className="care-intro"><span>03</span><div><h3>Главное в уходе</h3><p>{product.careInstructions || "Ориентируйтесь на состояние растения и грунта, а не только на календарь. Ниже — безопасная базовая памятка."}</p></div></article>
    </div>
    <section className="care-explorer" aria-labelledby="care-explorer-title"><div className="care-subheading"><span>04</span><div><p className="care-kicker">Нажмите на тему</p><h3 id="care-explorer-title">Уход без догадок</h3></div></div><div className="care-topic-tabs" role="tablist" aria-label="Темы ухода">{visualCare.map((item)=><button type="button" role="tab" aria-selected={item.key===activeTopic} className={item.key===activeTopic?"active":""} onClick={()=>setActiveTopic(item.key)} key={item.key}><strong>{item.title}</strong><small>{item.hint}</small></button>)}</div><article className="care-topic-panel" role="tabpanel"><img src={selected.image} alt="" loading="lazy"/><div><p className="care-kicker">{selected.hint}</p><h3>{selected.title}</h3><p>{topicText(passport, selected.key)}</p></div></article></section>
    {steps.length > 0 && <section className="care-routine"><div className="care-subheading"><span>05</span><h3>Регулярный уход</h3></div><div>{steps.map(([key,label])=><article key={key}><h4>{label}</h4><p>{passport[key]}</p></article>)}</div></section>}
    {(passport.problems || passport.pests || passport.toxicity) && <section className="care-troubleshooting"><div className="care-subheading"><span>06</span><h3>Если что-то пошло не так</h3></div><div>{passport.problems&&<article><h4>Листья подают сигнал</h4><p>{passport.problems}</p></article>}{passport.pests&&<article><h4>Вредители</h4><p>{passport.pests}</p></article>}{passport.toxicity&&<article><h4>Дети и питомцы</h4><p>{passport.toxicity}</p></article>}</div></section>}
    {faq.length > 0 && <section className="care-faq"><div className="care-subheading"><span>07</span><div><p className="care-kicker">Спрашивают чаще всего</p><h3>Короткие ответы</h3></div></div><div>{faq.map((item,index)=><details key={`${item.question}-${index}`}><summary>{item.question}</summary><p>{item.answer}</p></details>)}</div></section>}
  </section>;
}
