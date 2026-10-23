import { useId, useState } from "react";
import type { Product } from "./adminTypes";
import { money } from "./adminShared";

const normalize = (value: string) => value.toLocaleLowerCase("ru").replace(/ё/g, "е").trim();
export function AdminOrderProductSearch({
  products,
  value,
  disabled,
  onChange,
}: {
  products: Product[];
  value: string;
  disabled: boolean;
  onChange: (sku: string) => void;
}) {
  const id = useId();
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const terms = normalize(query).split(/\s+/).filter(Boolean);
  const matches = products.filter((product) =>
    terms.every((term) =>
      normalize(
        `${product.name} ${product.latinName ?? ""} ${product.variantLabel ?? ""} ${product.sku} ${product.productCode ?? ""}`,
      ).includes(term),
    ),
  );
  const results = matches.slice(0, 50);
  const selected = products.find((product) => product.sku === value);
  const expanded = open && !disabled;
  const choose = (product: Product) => {
    onChange(product.sku);
    setQuery("");
    setOpen(false);
    setActive(0);
  };
  return (
    <div
      className="order-product-search"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <input
        role="combobox"
        aria-label="Добавить товар в заказ"
        aria-autocomplete="list"
        aria-expanded={expanded}
        aria-controls={`${id}-results`}
        aria-activedescendant={expanded && results[active] ? `${id}-${active}` : undefined}
        autoComplete="off"
        disabled={disabled}
        placeholder="Поиск по названию или артикулу…"
        value={selected ? `${selected.name} · ${selected.sku}` : query}
        onFocus={() => {
          setOpen(true);
          setActive(0);
        }}
        onChange={(event) => {
          setQuery(event.target.value);
          onChange("");
          setOpen(true);
          setActive(0);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
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
        <div className="order-product-search-menu">
          <div role="listbox" id={`${id}-results`} aria-label="Найденные товары">
            {results.map((product, index) => (
              <button
                type="button"
                role="option"
                aria-selected={active === index}
                id={`${id}-${index}`}
                key={product.sku}
                tabIndex={-1}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => setActive(index)}
                onClick={() => choose(product)}
              >
                <strong>
                  {product.name}
                  {product.variantLabel && !product.name.includes(product.variantLabel)
                    ? ` · ${product.variantLabel}`
                    : ""}
                </strong>
                <small>
                  {product.sku} · {money.format(product.price)} · остаток {product.stock}
                </small>
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
