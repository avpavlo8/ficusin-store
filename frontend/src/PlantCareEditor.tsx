import { CareFieldDisclosure } from "./CareFieldDisclosure";
import { AutoTextarea } from "./AutoTextarea";
import type { PlantPassport } from "./adminTypes";

const careTopics = [
  ["lighting", "Свет и место"],
  ["watering", "Полив"],
  ["humidity", "Влажность воздуха"],
  ["temperature", "Температура"],
  ["soil", "Грунт"],
  ["repotting", "Пересадка"],
  ["fertilizer", "Подкормка"],
  ["growthRate", "Рост и развитие"],
  ["origin", "Происхождение"],
  ["careDifficulty", "Сложность ухода"],
  ["matureSize", "Взрослый размер"],
  ["toxicity", "Безопасность"],
  ["problems", "Типичные проблемы и решения"],
  ["pests", "Вредители"],
] as const;
export type CarePassport = Partial<Omit<PlantPassport, "faq">>;

export function PlantCareEditor({
  passport,
  onChange,
}: {
  passport: CarePassport;
  onChange: (passport: CarePassport) => void;
}) {
  return (
    <div className="editor-care-topics">
      {careTopics.map(([key, title]) => (
        <CareFieldDisclosure key={key} title={title} filled={Boolean(passport[key]?.trim())}>
          <AutoTextarea
            aria-label={title}
            rows={4}
            value={passport[key] || ""}
            onChange={(event) => onChange({ ...passport, [key]: event.target.value })}
          />
        </CareFieldDisclosure>
      ))}
    </div>
  );
}
