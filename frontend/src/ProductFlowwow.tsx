import { useEffect, useState } from "react";
import { api } from "./adminShared";
import type { FlowwowChannel, FlowwowField } from "./flowwowTypes";
const units: Record<number, string> = {
  1: "шт.",
  2: "г",
  3: "см",
  4: "мл",
  5: "унции",
  6: "дюймы",
  7: "мин.",
  8: "ч",
  9: "дни",
  10: "недели",
  11: "месяцы",
  12: "годы",
};
const managed = new Set([
  "offerId",
  "productType",
  "name",
  "description",
  "price",
  "discount",
  "images",
  "stock",
  "productionTime",
]);
export function ProductFlowwow({
  value,
  linked,
  sku,
  price,
  stock,
  onChange,
}: {
  value: FlowwowChannel;
  linked?: string;
  sku: string;
  price: string;
  stock: number;
  onChange: (value: FlowwowChannel) => void;
}) {
  const [categories, setCategories] = useState<Array<{ id: string; name: string }>>([]);
  const [fields, setFields] = useState<FlowwowField[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    const timer = setTimeout(() => {
      setLoading(true);
      setError("");
      void api<{
        data: Array<{
          name: string;
          subCategories: Array<{ subCategoryId: number; name: string }>;
        }>;
      }>("/api/v1/admin/flowwow/references?kind=categories")
        .then((data) => {
          if (active)
            setCategories(
              data.data.flatMap((group) =>
                group.subCategories.map((c) => ({
                  id: String(c.subCategoryId),
                  name: `${group.name} / ${c.name}`,
                })),
              ),
            );
        })
        .catch((e) => {
          if (active) setError((e as Error).message);
        })
        .finally(() => {
          if (active) setLoading(false);
        });
    }, 0);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [revision]);
  useEffect(() => {
    let active = true;
    const timer = setTimeout(() => {
      setFields([]);
      if (!value.category.id) return;
      setLoading(true);
      setError("");
      void api<{ data: FlowwowField[] }>(
        `/api/v1/admin/flowwow/references?kind=attributes&category=${encodeURIComponent(value.category.id)}`,
      )
        .then((data) => {
          if (active) setFields(data.data);
        })
        .catch((e) => {
          if (active) setError((e as Error).message);
        })
        .finally(() => {
          if (active) setLoading(false);
        });
    }, 0);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [value.category.id, revision]);
  const change = (patch: Partial<FlowwowChannel>) => onChange({ ...value, ...patch });
  const attribute = (field: string, v: unknown) => {
    const attributes = { ...value.attributes };
    if (v === undefined || v === "") delete attributes[field];
    else attributes[field] = v;
    change({ attributes });
  };
  const control = (f: FlowwowField) => {
    const v = value.attributes?.[f.field];
    if (f.type === "bool")
      return (
        <select
          value={v === undefined ? "" : String(v)}
          onChange={(e) =>
            attribute(f.field, e.target.value === "" ? undefined : e.target.value === "true")
          }
        >
          <option value="">Не задано</option>
          <option value="true">Да</option>
          <option value="false">Нет</option>
        </select>
      );
    if (f.type === "select" || f.type === "multiselect")
      return (
        <select
          multiple={f.type === "multiselect"}
          value={
            f.type === "multiselect" ? (Array.isArray(v) ? v.map(String) : []) : String(v ?? "")
          }
          onChange={(e) => {
            const ids = Array.from(e.target.selectedOptions)
              .map((o) => f.values?.find((x) => String(x.id) === o.value)?.id)
              .filter((x) => x !== undefined);
            attribute(f.field, f.type === "select" ? ids[0] : ids);
          }}
        >
          {f.type === "select" && <option value="">Выберите значение</option>}
          {f.values?.map((o) => (
            <option key={o.id} value={String(o.id)}>
              {o.name}
            </option>
          ))}
        </select>
      );
    if (f.type === "measure") {
      const m = (v || {}) as { val?: number; measure?: number };
      return (
        <span className="admin-form-grid">
          <input
            aria-label={`${f.name}: значение`}
            type="number"
            min={f.minValue}
            max={f.maxValue}
            step="any"
            value={m.val ?? ""}
            onChange={(e) =>
              attribute(
                f.field,
                e.target.value === ""
                  ? undefined
                  : { val: Number(e.target.value), measure: m.measure ?? f.measure?.[0]?.id },
              )
            }
          />
          <select
            aria-label={`${f.name}: единица измерения`}
            value={m.measure ?? f.measure?.[0]?.id ?? ""}
            onChange={(e) => attribute(f.field, { ...m, measure: Number(e.target.value) })}
          >
            {f.measure?.map((u) => (
              <option key={u.id} value={u.id}>
                {units[u.id] || String(u.id)}
              </option>
            ))}
          </select>
        </span>
      );
    }
    if (f.type === "consist")
      return (
        <FlowwowComposition
          value={Array.isArray(v) ? v : []}
          units={f.measure || []}
          onChange={(v) => attribute(f.field, v)}
        />
      );
    if (f.type === "multitext")
      return (
        <textarea
          value={Array.isArray(v) ? v.join("\n") : ""}
          placeholder="Каждое значение с новой строки"
          onChange={(e) => attribute(f.field, e.target.value.split("\n").filter(Boolean))}
        />
      );
    if (f.type === "decimal")
      return (
        <input
          type="number"
          step="any"
          min={f.minValue}
          max={f.maxValue}
          value={typeof v === "number" ? v : ""}
          onChange={(e) =>
            attribute(f.field, e.target.value === "" ? undefined : Number(e.target.value))
          }
        />
      );
    if (f.type === "text")
      return (
        <input
          value={typeof v === "string" ? v : ""}
          onChange={(e) => attribute(f.field, e.target.value)}
        />
      );
    return <span role="status">Поле пока не поддерживается. Обратитесь к администратору.</span>;
  };
  return (
    <section className="editor-channel-panel" aria-label="Flowwow">
      <h3>Flowwow</h3>
      <p className="editor-secondary">
        Название, описание, фотографии и цена берутся с сайта. Остаток — из складского учёта сайта.
      </p>
      <p>
        Цена: {price || "—"} ₽ · Остаток: {stock} шт.
      </p>
      {linked ? (
        <p role="status">
          Карточка связана · Артикул {linked}. При сохранении обновится в Flowwow.
        </p>
      ) : (
        <div className="admin-form-grid">
          <label>
            <input
              type="checkbox"
              checked={value.enabled}
              onChange={(e) => change({ enabled: e.target.checked })}
            />
            Отправлять в Flowwow
          </label>
          <label>
            Действие
            <select
              value={value.mode}
              onChange={(e) => change({ mode: e.target.value as "create" | "link" })}
            >
              <option value="create">Создать карточку</option>
              <option value="link">Связать существующую</option>
            </select>
          </label>
          {value.mode === "link" && (
            <label>
              ID существующей карточки Flowwow
              <input
                inputMode="numeric"
                value={value.existingId}
                onChange={(e) => change({ existingId: e.target.value })}
              />
            </label>
          )}
          <label>
            Артикул Flowwow
            <input
              value={value.article ?? sku}
              maxLength={50}
              onChange={(e) => change({ article: e.target.value })}
            />
          </label>
        </div>
      )}
      <label>
        Подкатегория Flowwow
        <select
          disabled={Boolean(linked && value.category.id)}
          value={value.category.id}
          onChange={(e) =>
            change({
              category: categories.find((c) => c.id === e.target.value) || { id: "", name: "" },
              attributes: {},
            })
          }
        >
          <option value="">Выберите подкатегорию</option>
          {categories.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>
      </label>
      {loading && <p role="status">Загружаем поля Flowwow…</p>}
      {error && (
        <p role="alert">
          {error}{" "}
          <button type="button" className="admin-action" onClick={() => setRevision((v) => v + 1)}>
            Повторить загрузку
          </button>
        </p>
      )}
      <div className="admin-form-grid">
        {fields
          .filter((f) => !managed.has(f.field))
          .map((f) => (
            <label key={f.field}>
              {f.name}
              {f.required ? " *" : ""}
              {control(f)}
              {f.dependsOn?.length ? (
                <small>Заполните это поле либо альтернативное: {f.dependsOn.join(", ")}.</small>
              ) : null}
            </label>
          ))}
      </div>
    </section>
  );
}
function FlowwowComposition({
  value,
  units: options,
  onChange,
}: {
  value: unknown[];
  units: Array<{ id: number }>;
  onChange: (v: unknown[]) => void;
}) {
  const rows = value as Array<{ name: string; quantity: number; measure: number }>;
  return (
    <span>
      {rows.map((row, i) => (
        <span key={i} className="admin-form-grid">
          <input
            aria-label={`Состав ${i + 1}: название`}
            value={row.name}
            onChange={(e) =>
              onChange(rows.map((r, n) => (n === i ? { ...r, name: e.target.value } : r)))
            }
          />
          <input
            aria-label={`Состав ${i + 1}: количество`}
            type="number"
            min="1"
            value={row.quantity}
            onChange={(e) =>
              onChange(
                rows.map((r, n) => (n === i ? { ...r, quantity: Number(e.target.value) } : r)),
              )
            }
          />
          <select
            aria-label={`Состав ${i + 1}: единица`}
            value={row.measure}
            onChange={(e) =>
              onChange(
                rows.map((r, n) => (n === i ? { ...r, measure: Number(e.target.value) } : r)),
              )
            }
          >
            {options.map((o) => (
              <option key={o.id} value={o.id}>
                {units[o.id]}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="admin-action"
            onClick={() => onChange(rows.filter((_, n) => n !== i))}
          >
            Удалить компонент
          </button>
        </span>
      ))}
      <button
        type="button"
        className="admin-action"
        onClick={() => onChange([...rows, { name: "", quantity: 1, measure: options[0]?.id ?? 1 }])}
      >
        Добавить компонент
      </button>
    </span>
  );
}
