import type { PlantPassportData } from "./types";

const sections = [
  { id: "environment", title: "Среда", keys: [["origin", "Происхождение"], ["lighting", "Освещение"], ["humidity", "Влажность"], ["temperature", "Температура"]] },
  { id: "routine-care", title: "Регулярный уход", keys: [["watering", "Полив"], ["soil", "Грунт"], ["fertilizer", "Удобрение"], ["repotting", "Пересадка"]] },
  { id: "growth-safety", title: "Рост и безопасность", keys: [["careDifficulty", "Сложность ухода"], ["growthRate", "Скорость роста"], ["matureSize", "Взрослый размер"], ["toxicity", "Токсичность"]] },
  { id: "diagnostics", title: "Диагностика", keys: [["problems", "Типичные проблемы и решения"], ["pests", "Вредители"]] },
] as const;

export function PlantPassport({ name, passport }: { name: string; passport: PlantPassportData }) {
  const groups = sections.map((group) => ({ ...group, rows: group.keys.filter(([key]) => passport[key]) })).filter((group) => group.rows.length);
  return <section className="plant-passport pdp-section pdp-info-card" id="plant-passport"><header className="pdp-section-heading"><h2>Паспорт растения</h2></header>
    {groups.length ? <div className="passport-sections">{groups.map((group) => <section id={`passport-${group.id}`} key={group.id}><h3>{group.title}</h3><dl>{group.rows.map(([key, label]) => <div key={key}><dt>{label}</dt><dd>{passport[key]}</dd></div>)}</dl></section>)}</div> : <div className="passport-empty"><strong>Паспорт готовится</strong><p>Рекомендации для {name} появятся после проверки редактором каталога.</p></div>}
    {(passport.faq || []).length > 0 && <div className="passport-faq"><h3>Частые вопросы</h3>{passport.faq!.map((item, index) => <details key={`${item.question}-${index}`}><summary>{item.question}</summary><p>{item.answer}</p></details>)}</div>}
  </section>;
}
