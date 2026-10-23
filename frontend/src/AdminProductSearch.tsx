import { useId, useState } from "react";
import "./styles/admin-product-search.css";

export type AdminProductOption = {
  value: string;
  name: string;
  detail: string;
  search?: string;
};

const normalize = (value: string) => value.toLocaleLowerCase("ru").replace(/ё/g, "е").trim();
export function AdminProductSearch({
  options,
  value,
  disabled = false,
  label,
  placeholder = "Поиск по названию или артикулу…",
  clearOnSearch = false,
  onChange,
}: {
  options: AdminProductOption[];
  value: string;
  disabled?: boolean;
  label: string;
  placeholder?: string;
  clearOnSearch?: boolean;
  onChange: (value: string) => void;
}) {
  const id = useId();
  const [editing, setEditing] = useState(false);
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const terms = normalize(query).split(/\s+/).filter(Boolean);
  const matches = options.filter((option) =>
    terms.every((term) =>
      normalize(`${option.name} ${option.detail} ${option.search ?? ""}`).includes(term),
    ),
  );
  const results = matches.slice(0, 50);
  const selected = options.find((option) => option.value === value);
  const expanded = open && !disabled;
  const choose = (product: AdminProductOption) => {
    onChange(product.value);
    setEditing(false);
    setQuery("");
    setOpen(false);
    setActive(0);
  };
  return (
    <div
      className="admin-product-search"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) {
          setOpen(false);
          if (selected) {
            setEditing(false);
            setQuery("");
          }
        }
      }}
    >
      <input
        role="combobox"
        aria-label={label}
        aria-autocomplete="list"
        aria-expanded={expanded}
        aria-controls={`${id}-results`}
        aria-activedescendant={expanded && results[active] ? `${id}-${active}` : undefined}
        autoComplete="off"
        disabled={disabled}
        placeholder={placeholder}
        value={selected && !editing ? `${selected.name} · ${selected.detail}` : query}
        onFocus={() => {
          setOpen(true);
          setActive(0);
        }}
        onChange={(event) => {
          setQuery(event.target.value);
          setEditing(true);
          if (clearOnSearch) onChange("");
          setOpen(true);
          setActive(0);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape" && expanded) {
            event.stopPropagation();
            setEditing(false);
            setQuery("");
            setOpen(false);
            return;
          }
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            setOpen(true);
            setActive((current) =>
              Math.max(
                0,
                Math.min(
                  results.length - 1,
                  expanded ? current + (event.key === "ArrowDown" ? 1 : -1) : 0,
                ),
              ),
            );
          }
          if (event.key === "Enter" && expanded) {
            event.preventDefault();
            if (results[active]) choose(results[active]);
          }
        }}
      />
      {expanded && (
        <div className="admin-product-search-menu">
          <div role="listbox" id={`${id}-results`} aria-label="Найденные товары">
            {results.map((product, index) => (
              <button
                type="button"
                role="option"
                aria-selected={active === index}
                id={`${id}-${index}`}
                key={product.value}
                tabIndex={-1}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => setActive(index)}
                onClick={() => choose(product)}
              >
                <strong>{product.name}</strong>
                <small>{product.detail}</small>
              </button>
            ))}
          </div>
          {!results.length && (
            <p role="status">Товар не найден. Попробуйте другое название или артикул.</p>
          )}
          {matches.length > results.length && (
            <p role="status">Показаны первые 50 из {matches.length}. Уточните поиск.</p>
          )}
        </div>
      )}
    </div>
  );
}
