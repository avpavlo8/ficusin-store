import type { ProductDetail } from "./types";

const quickFacts = [
  ["☀", "Освещение", "lighting"], ["◌", "Полив", "watering"],
  ["≈", "Влажность", "humidity"], ["°", "Температура", "temperature"],
] as const;

const routine = [
  ["watering", "Когда поливать"], ["soil", "Какой грунт"],
  ["fertilizer", "Чем подкармливать"], ["repotting", "Когда пересаживать"],
] as const;

const visualCare = [
  ["lighting", "Свет", "/images/care/light.webp"],
  ["watering", "Полив", "/images/care/watering.webp"],
  ["humidity", "Влажность", "/images/care/humidity.webp"],
  ["repotting", "Пересадка", "/images/care/repotting.webp"],
] as const;

export function PlantCareGuide({ product }: { product: ProductDetail }) {
  const passport = product.passport || {};
  const facts = quickFacts.filter(([, , key]) => passport[key]);
  const steps = routine.filter(([key]) => passport[key]);
  return <section className="care-guide pdp-info-card" id="care-guide">
    <header className="care-guide-heading"><div><p>Инструкция Фикусина</p><h2>Как ухаживать за {product.name}</h2></div><span>Сохраните страницу — рекомендации всегда будут под рукой</span></header>
    {facts.length > 0 && <div className="care-quick-facts">{facts.map(([icon,label,key])=><article key={key}><i aria-hidden="true">{icon}</i><small>{label}</small><strong>{passport[key]}</strong></article>)}</div>}
    {product.importantWarnings.length > 0 && <aside className="care-warning"><strong>Важно знать</strong><ul>{product.importantWarnings.map((warning)=><li key={warning}>{warning}</li>)}</ul></aside>}
    <div className="care-guide-body">
      <article className="care-intro"><span>01</span><div><h3>Знакомство с растением</h3><p>{product.description || "Описание растения готовится."}</p></div></article>
      {product.careInstructions && <article className="care-intro"><span>02</span><div><h3>Главное в уходе</h3><p>{product.careInstructions}</p></div></article>}
    </div>
    <div className="care-visual-steps">{visualCare.filter(([key])=>passport[key]).map(([key,label,image],index)=><article key={key}><img src={image} alt="" loading="lazy"/><div><small>{String(index+1).padStart(2,"0")}</small><h3>{label}</h3><p>{passport[key]}</p></div></article>)}</div>
    {steps.length > 0 && <section className="care-routine"><div className="care-subheading"><span>03</span><h3>Регулярный уход</h3></div><div>{steps.map(([key,label])=><article key={key}><h4>{label}</h4><p>{passport[key]}</p></article>)}</div></section>}
    {(passport.problems || passport.pests || passport.toxicity) && <section className="care-troubleshooting"><div className="care-subheading"><span>04</span><h3>Если что-то пошло не так</h3></div><div>{passport.problems&&<article><h4>Типичные сигналы</h4><p>{passport.problems}</p></article>}{passport.pests&&<article><h4>Вредители</h4><p>{passport.pests}</p></article>}{passport.toxicity&&<article><h4>Безопасность</h4><p>{passport.toxicity}</p></article>}</div></section>}
  </section>;
}
