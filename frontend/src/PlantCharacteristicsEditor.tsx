import type { CategoryAttribute } from "./adminTypes";
import { NumericInput } from "./NumericInput";
import { EditorMultiSelect } from "./EditorMultiSelect";
import { AutoTextarea } from "./AutoTextarea";
import { CharacteristicIcon } from "./product/CharacteristicIcon";

const rows = [
  ["light_level", "Освещение", "light"],
  ["watering", "Полив", "water"],
  ["height_cm", "Высота", "height"],
  ["pot_diameter_cm", "Диаметр горшка", "pot"],
  ["care_level", "Сложность ухода", "care"],
  ["pet_safety", "Питомцы", "pets"],
  ["humidity", "Влажность воздуха", "humidity"],
  ["wintering_type", "Зимовка", "care"],
  ["flowering", "Цветущее", "care"],
  ["growth_habit", "Форма роста", "care"],
  ["leaf_coloring", "Окраска листьев", "care"],
  ["fruiting", "Плодовое растение", "care"],
] as const;

type AttributeValue = string | number | boolean | string[] | null;
export function PlantCharacteristicsEditor({
  schema,
  values,
  onChange,
}: {
  schema: CategoryAttribute[];
  values: Record<string, unknown>;
  onChange: (code: string, value: AttributeValue) => void;
}) {
  return (
    <div className="editor-plant-characteristics">
      {rows.map(([code, title, icon]) => {
        const field = schema.find((item) => item.code === code);
        if (!field) return null;
        const value = values[field.code];
        const id = `plant-characteristic-${code}`;
        const titleText = `${title}${field.unit ? `, ${field.unit}` : ""}`;
        const selected = Array.isArray(value)
          ? value.map(String)
          : typeof value === "string" && value
            ? [value]
            : [];
        return (
          <div key={field.code} className="editor-plant-characteristic">
            <CharacteristicIcon name={icon} />
            {field.dataType === "multi_enum" ? (
              <span>{titleText}</span>
            ) : (
              <label htmlFor={id}>{titleText}</label>
            )}
            {field.dataType === "multi_enum" ? (
              <EditorMultiSelect
                compact
                label={title}
                options={[...new Set([...field.options, ...selected])].map((option) => ({
                  value: option,
                  label: field.optionLabels?.[option] || option,
                }))}
                value={selected}
                onChange={(value) => onChange(code, value)}
              />
            ) : field.dataType === "boolean" ? (
              <select
                id={id}
                aria-label={title}
                value={typeof value === "boolean" ? String(value) : ""}
                onChange={(event) =>
                  onChange(code, event.target.value === "" ? null : event.target.value === "true")
                }
              >
                <option value="">Не указано</option>
                <option value="true">Да</option>
                <option value="false">Нет</option>
              </select>
            ) : field.dataType === "enum" ? (
              <select
                id={id}
                aria-label={title}
                value={String(value ?? "")}
                onChange={(event) => onChange(code, event.target.value || null)}
              >
                <option value="">Не указано</option>
                {value && !field.options.includes(String(value)) ? (
                  <option value={String(value)}>{String(value)}</option>
                ) : null}
                {field.options.map((option) => (
                  <option key={option} value={option}>
                    {field.optionLabels?.[option] || option}
                  </option>
                ))}
              </select>
            ) : field.dataType === "number" ? (
              <NumericInput
                id={id}
                aria-label={title}
                min="0"
                step="any"
                value={String(value ?? "")}
                onChange={(event) =>
                  onChange(code, event.target.value === "" ? null : Number(event.target.value))
                }
              />
            ) : (
              <AutoTextarea
                id={id}
                aria-label={title}
                rows={2}
                value={String(value ?? "")}
                onChange={(event) => onChange(code, event.target.value || null)}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}
