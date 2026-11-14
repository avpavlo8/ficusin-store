import { useEffect, useRef, useState } from "react";
import { api } from "./adminShared";
import { flattenCategories, type ChannelCategory } from "./catalogEditorChannels";
export function ChannelReferencePicker({
  label,
  ariaLabel,
  value,
  onChange,
  channel,
  items,
}: {
  label: string;
  ariaLabel?: string;
  value: ChannelCategory;
  onChange: (v: ChannelCategory) => void;
  channel: "ozon" | "wb" | "saby";
  items?: ChannelCategory[];
}) {
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [options, setOptions] = useState<ChannelCategory[]>(items || []),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const close = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, []);
  const load = () => {
    setOpen(true);
    setQuery("");
    if (items || options.length || busy) return;
    setBusy(true);
    setError("");
    api<{ data: unknown }>(
      `/api/v1/admin/catalog-editor/references?channel=${channel}&kind=categories`,
    )
      .then((r) => setOptions(flattenCategories(channel === "wb" ? "wb" : "ozon", r.data)))
      .catch((e) => setError(e.message))
      .finally(() => setBusy(false));
  };
  useEffect(() => {
    if (channel !== "wb" || !open || query.trim().length < 2) return;
    let active = true;
    const timer = setTimeout(() => {
      api<{ data: unknown }>(
        `/api/v1/admin/catalog-editor/references?channel=wb&kind=categories&q=${encodeURIComponent(query.trim())}`,
      )
        .then((r) => {
          if (active) {
            setOptions(flattenCategories("wb", r.data));
            setError("");
          }
        })
        .catch((e) => {
          if (active) setError(e.message);
        });
    }, 350);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [channel, open, query]);
  const available = (items || options).filter((o) =>
    o.name.toLocaleLowerCase("ru").includes(query.toLocaleLowerCase("ru")),
  );
  return (
    <label className="editor-reference-label">
      <span>{label}</span>
      <div className="editor-reference-picker" ref={root}>
        <input
          aria-label={ariaLabel || label}
          role="combobox"
          aria-expanded={open}
          value={open ? query : value.name}
          placeholder="Выбрать из справочника"
          onFocus={load}
          onChange={(e) => {
            setQuery(e.target.value);
            setOpen(true);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") setOpen(false);
          }}
        />
        {open && (
          <div className="editor-reference-options">
            {busy ? (
              <p>Загружаем…</p>
            ) : error ? (
              <p role="status">
                {error}
                <button type="button" className="admin-action" onClick={load}>
                  Повторить
                </button>
              </p>
            ) : available.length ? (
              available.slice(0, 100).map((o) => (
                <button
                  type="button"
                  key={`${o.id}:${o.typeId || ""}`}
                  onClick={() => {
                    onChange(o);
                    setOpen(false);
                  }}
                >
                  {o.name}
                  {o.id === value.id && o.typeId === value.typeId ? " ✓" : ""}
                </button>
              ))
            ) : (
              <p>Нет доступных значений</p>
            )}
          </div>
        )}
      </div>
    </label>
  );
}
