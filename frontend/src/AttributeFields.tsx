import { NumericInput } from "./NumericInput";
import type { CategoryAttribute } from "./adminTypes";
export function AttributeFields({
  schema,
  values,
  onChange,
}: {
  schema: CategoryAttribute[];
  values: Record<string, unknown>;
  onChange: (code: string, value: string | number | boolean | string[] | null) => void;
}) {
  const field = (attribute: CategoryAttribute) => {
    const value = values[attribute.code];
    const title = `${attribute.name}${attribute.unit ? `, ${attribute.unit}` : ""}${attribute.required ? " *" : ""}`;
    if (attribute.dataType === "boolean")
      return (
        <label className="attribute-control" key={attribute.code}>
          <span>{title}</span>
          <select
            aria-label={title}
            value={value == null ? "" : String(value)}
            onChange={(event) =>
              onChange(
                attribute.code,
                event.target.value === "" ? null : event.target.value === "true",
              )
            }
          >
            <option value="">Не указано</option>
            <option value="true">Да</option>
            <option value="false">Нет</option>
          </select>
        </label>
      );
    if (attribute.dataType === "enum") {
      const current = String(value ?? "");
      const unknown = current && !attribute.options.includes(current);
      return (
        <label className="attribute-control" key={attribute.code}>
          <span>{title}</span>
          <select
            aria-label={title}
            required={attribute.required}
            value={current}
            onChange={(event) => onChange(attribute.code, event.target.value || null)}
          >
            <option value="">Не указано</option>
            {unknown && <option value={current}>{`Неизвестное значение: ${current}`}</option>}
            {attribute.options.map((option) => (
              <option key={option} value={option}>
                {attribute.optionLabels?.[option] || option}
              </option>
            ))}
          </select>
          {unknown && (
            <small className="admin-inline-error">
              Старое значение сохранено. Выберите вариант из матрицы, чтобы заменить его.
            </small>
          )}
        </label>
      );
    }
    if (attribute.dataType === "multi_enum") {
      const selected = Array.isArray(value) ? value.map(String) : [];
      const unknown = selected.filter((option) => !attribute.options.includes(option));
      return (
        <fieldset className="attribute-multi" key={attribute.code}>
          <legend>{title}</legend>
          <div className="attribute-choice-list">
            {attribute.options.map((option) => (
              <label className={selected.includes(option) ? "selected" : ""} key={option}>
                <input
                  type="checkbox"
                  checked={selected.includes(option)}
                  onChange={(event) =>
                    onChange(
                      attribute.code,
                      event.target.checked
                        ? [...selected, option]
                        : selected.filter((item) => item !== option),
                    )
                  }
                />
                {attribute.optionLabels?.[option] || option}
              </label>
            ))}
          </div>
          {unknown.length > 0 && (
            <small className="admin-inline-error">
              Неизвестные старые значения сохранены: {unknown.join(", ")}
            </small>
          )}
        </fieldset>
      );
    }
    return (
      <label className="attribute-control" key={attribute.code}>
        <span>{title}</span>
        <NumericInput
          aria-label={title}
          type={attribute.dataType === "number" ? "number" : "text"}
          min={attribute.dataType === "number" ? 0 : undefined}
          step={attribute.dataType === "number" ? "any" : undefined}
          required={attribute.required}
          value={value == null ? "" : String(value)}
          onChange={(event) =>
            onChange(
              attribute.code,
              event.target.value === ""
                ? null
                : attribute.dataType === "number"
                  ? Number(event.target.value)
                  : event.target.value,
            )
          }
        />
      </label>
    );
  };
  const group = (attribute: CategoryAttribute) => {
    if (/^(package_|shipping_)/.test(attribute.code)) return "Упаковка и логистика";
    if (/(height|width|length|diameter|pot|size)/.test(attribute.code)) return "Размеры растения";
    if (/(light|water|care|humidity|temperature|soil|pet|toxic|growth)/.test(attribute.code))
      return "Уход";
    return "Характеристики";
  };
  const groups = ["Уход", "Размеры растения", "Упаковка и логистика", "Характеристики"]
    .map((title) => ({ title, items: schema.filter((item) => group(item) === title) }))
    .filter((item) => item.items.length > 0);
  return (
    <div className="attribute-groups wide">
      {groups.map(({ title, items }) => (
        <div className="attribute-group" key={title}>
          <h4>{title}</h4>
          <div className="attribute-control-grid">{items.map(field)}</div>
        </div>
      ))}
    </div>
  );
}
