import { NumericInput } from "./NumericInput";
import type { CategoryAttribute } from "./adminTypes";

export function ProductPackagingEditor({
  schema,
  values,
  onChange,
}: {
  schema: CategoryAttribute[];
  values: Record<string, unknown>;
  onChange: (code: string, value: number | null) => void;
}) {
  const field = (code: string, label: string, ariaLabel = label) =>
    schema.some((item) => item.code === code) ? (
      <label key={code}>
        {label}
        <NumericInput
          aria-label={ariaLabel}
          min="0"
          step="any"
          value={String(values[code] ?? "")}
          onChange={(event) =>
            onChange(code, event.target.value === "" ? null : Number(event.target.value))
          }
        />
      </label>
    ) : null;
  return (
    <div className="editor-packaging">
      {field("package_weight_grams", "Вес, г")}
      {([false, true] as const).map((winter) => (
        <div key={String(winter)}>
          <h3>{winter ? "Зимняя коробка" : "Летняя упаковка"}</h3>
          <div className="editor-package-dimensions">
            {field(
              winter ? "package_winter_length_cm" : "package_length_cm",
              "Длина, см",
              winter ? "Длина зимней коробки, см" : "Длина летней упаковки, см",
            )}
            <span aria-hidden="true">×</span>
            {field(
              winter ? "package_winter_width_cm" : "package_width_cm",
              "Ширина, см",
              winter ? "Ширина зимней коробки, см" : "Ширина летней упаковки, см",
            )}
            <span aria-hidden="true">×</span>
            {field(
              winter ? "package_winter_height_cm" : "package_height_cm",
              "Высота, см",
              winter ? "Высота зимней коробки, см" : "Высота летней упаковки, см",
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
