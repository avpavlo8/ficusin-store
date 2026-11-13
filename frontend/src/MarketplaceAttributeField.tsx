import { useEffect, useRef, useState } from "react";
import { api } from "./adminShared";
import { AutoTextarea } from "./AutoTextarea";
import { NumericInput } from "./NumericInput";
import type {
  ChannelCategory,
  MarketplaceAttributeValue,
  OzonAttribute,
} from "./catalogEditorChannels";

import { attributeValues } from "./marketplaceAttributes";
type Choice = { id?: number; value: string };
function choicesOf(raw: unknown): { choices: Choice[]; cursor: string; freeText: boolean } {
  const data = raw as {
    result?: Array<{ id: number; value: string }>;
    brands?: Array<{ id: number; name: string }>;
    data?: unknown[];
    next?: number;
    has_next?: boolean;
    freeText?: boolean;
  };
  const values = data.result || data.brands || data.data || [];
  const choices = values
    .map((item) => {
      if (typeof item === "string" || typeof item === "number") return { value: String(item) };
      const v = item as Record<string, unknown>;
      return {
        id: typeof v.id === "number" ? v.id : undefined,
        value: String(v.value ?? v.name ?? v.tnved ?? v.rate ?? v.vat ?? ""),
      };
    })
    .filter((v) => v.value);
  return {
    choices,
    cursor: data.next
      ? String(data.next)
      : data.has_next && choices.length
        ? String(choices.at(-1)?.id || "")
        : "",
    freeText: !!data.freeText,
  };
}
export function MarketplaceDictionary({
  label,
  channel,
  category,
  attribute,
  kind = "values",
  selected,
  multiple = false,
  onChange,
}: {
  label: string;
  channel: "wb" | "ozon";
  category: ChannelCategory;
  attribute?: number;
  kind?: "values" | "brand" | "tnved";
  selected: Choice[];
  multiple?: boolean;
  onChange: (values: Choice[]) => void;
}) {
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [choices, setChoices] = useState<Choice[]>([]),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [cursor, setCursor] = useState("");
  const root = useRef<HTMLDivElement>(null),
    sequence = useRef(0);
  const load = async (q: string, next = "") => {
    const seq = ++sequence.current;
    setBusy(true);
    setError("");
    try {
      if (!category.id)
        throw new Error(`Сначала выберите категорию ${channel === "wb" ? "Wildberries" : "Ozon"}.`);
      const params = new URLSearchParams({
        channel,
        kind,
        category: category.id,
        typeId: String(category.typeId || 0),
        attribute: String(attribute || 0),
        q,
        cursor: next,
      });
      const response = await api<{ data: unknown }>(
        `/api/v1/admin/catalog-editor/references?${params}`,
      );
      if (seq !== sequence.current) return;
      const result = choicesOf(response.data);
      setChoices((old) =>
        next
          ? [
              ...old,
              ...result.choices.filter(
                (v) => !old.some((o) => o.value === v.value && o.id === v.id),
              ),
            ]
          : result.choices,
      );
      setCursor(result.cursor);
    } catch (e) {
      if (seq === sequence.current) {
        setError((e as Error).message);
        setChoices([]);
      }
    } finally {
      if (seq === sequence.current) setBusy(false);
    }
  };
  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    return () => {
      document.removeEventListener("pointerdown", close);
    };
  }, []);
  return (
    <div className="editor-reference-label" ref={root}>
      <label>
        {label}
        <input
          role="combobox"
          aria-expanded={open}
          autoComplete="off"
          value={open ? query : selected.map((v) => v.value).join(", ")}
          placeholder="Выбрать из справочника"
          onFocus={() => {
            setOpen(true);
            setQuery("");
            void load("");
          }}
          onChange={(e) => {
            setQuery(e.target.value);
            void load(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") setOpen(false);
            if (e.key === "Backspace" && !query && selected.length)
              onChange(multiple ? selected.slice(0, -1) : []);
          }}
        />
      </label>
      {open && (
        <div
          className="editor-reference-options"
          role="listbox"
          aria-label={label}
          aria-multiselectable={multiple}
        >
          {busy && <p role="status">Загрузка…</p>}
          {error && (
            <>
              <p role="alert">{error}</p>
              <button type="button" onClick={() => void load(query)}>
                Повторить загрузку
              </button>
            </>
          )}
          {!busy && !error && choices.length === 0 && <p>Нет подходящих значений</p>}
          {choices
            .filter((v) => channel !== "wb" || v.value.toLowerCase().includes(query.toLowerCase()))
            .map((v) => {
              const checked = selected.some((s) => s.value === v.value && (!v.id || s.id === v.id));
              return (
                <button
                  key={`${v.id || ""}:${v.value}`}
                  role="option"
                  aria-selected={checked}
                  type="button"
                  onClick={() => {
                    onChange(
                      multiple
                        ? checked
                          ? selected.filter((s) => s.value !== v.value)
                          : [...selected, v]
                        : [v],
                    );
                    if (!multiple) setOpen(false);
                  }}
                >
                  <span className="editor-dictionary-check" aria-hidden="true">
                    {checked ? "✓" : ""}
                  </span>
                  {v.value}
                </button>
              );
            })}
          {cursor && (
            <button type="button" disabled={busy} onClick={() => void load(query, cursor)}>
              Показать ещё
            </button>
          )}
        </div>
      )}
    </div>
  );
}
export function MarketplaceAttributeField({
  channel,
  category,
  attribute: a,
  value,
  onChange,
}: {
  channel: "wb" | "ozon";
  category: ChannelCategory;
  attribute: OzonAttribute;
  value?: MarketplaceAttributeValue;
  onChange: (value: MarketplaceAttributeValue) => void;
}) {
  const selected = attributeValues(value),
    max = a.max_value_count || a.maxCount || 0;
  const dictionary =
    channel === "ozon" ? !!a.dictionary_id : /страна|цвет|сезон|^пол$|ндс|тн.*вэд/i.test(a.name);
  const label = `${a.name}${a.is_required || a.required ? " *" : ""}`;
  if (dictionary)
    return (
      <MarketplaceDictionary
        label={label}
        channel={channel}
        category={category}
        attribute={a.id || a.charcID}
        selected={selected}
        multiple={max !== 1}
        onChange={(values) =>
          onChange(values.map((v) => ({ value: v.value, dictionaryValueId: v.id })))
        }
      />
    );
  if (a.type === "Boolean")
    return (
      <label>
        {label}
        <select value={selected[0]?.value || ""} onChange={(e) => onChange(e.target.value)}>
          <option value="">Выберите</option>
          <option value="true">Да</option>
          <option value="false">Нет</option>
        </select>
      </label>
    );
  if (a.charcType === 4 || a.type === "Integer" || a.type === "Decimal")
    return (
      <label>
        {label}
        <NumericInput
          step={a.type === "Integer" ? "1" : "any"}
          value={selected[0]?.value || ""}
          onChange={(e) => onChange(e.target.value)}
        />
      </label>
    );
  return (
    <label>
      {label}
      <AutoTextarea
        rows={1}
        value={selected.map((v) => v.value).join("\n")}
        onChange={(e) =>
          onChange(
            max === 1 ? e.target.value : e.target.value.split("\n").map((value) => ({ value })),
          )
        }
      />
    </label>
  );
}
