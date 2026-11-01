import { useState } from "react";
import { categoryPath, normalizeCatalogQuery } from "./adminCatalogLogic";
import type { Category } from "./adminTypes";

// Flattens the category tree into the order it reads in: each parent is
// followed by its own children, siblings sorted by sortOrder and then by
// name. Every consumer that lists categories uses this, so the admin sees
// the same order in the tree, in the parent picker and on a product card.
export function orderCategoryTree(items: Category[]): { item: Category; depth: number }[] {
  const result: { item: Category; depth: number }[] = [];
  const append = (parentId: number | null, depth: number) => {
    items
      .filter((item) => item.parentId === parentId)
      .sort(
        (left, right) =>
          left.sortOrder - right.sortOrder || left.name.localeCompare(right.name, "ru"),
      )
      .forEach((item) => {
        result.push({ item, depth });
        append(item.id, depth + 1);
      });
  };
  append(null, 0);
  // Categories whose parent is missing would otherwise vanish from the list.
  items
    .filter(
      (item) => item.parentId !== null && !items.some((parent) => parent.id === item.parentId),
    )
    .sort((left, right) => left.name.localeCompare(right.name, "ru"))
    .forEach((item) => result.push({ item, depth: 0 }));
  return result;
}

// The category list is long, so a product card shows only the current
// choice and reveals the tree on demand instead of an always-open list.
export function CategoryPicker({
  categories,
  value,
  onChange,
}: {
  categories: Category[];
  value?: number;
  onChange: (value?: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const ordered = orderCategoryTree(categories);
  const selected = categories.find((item) => item.id === value);
  // Only branches the user opened are listed, so the picker shows a handful
  // of sections instead of every leaf in the catalogue.
  const visible = ordered.filter(({ item }) => {
    if (query.trim())
      return normalizeCatalogQuery(categoryPath(categories, item.id)).includes(
        normalizeCatalogQuery(query),
      );
    let parent = item.parentId;
    while (parent) {
      if (!expanded.has(parent)) return false;
      parent = categories.find((candidate) => candidate.id === parent)?.parentId ?? null;
    }
    return true;
  });
  const toggle = (id: number) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  return (
    <div className="category-picker">
      <button
        type="button"
        className="category-picker-toggle"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
      >
        <span>{selected ? categoryPath(categories, selected.id) : "Выбрать категорию"}</span>
        <span aria-hidden="true">{open ? "−" : "+"}</span>
      </button>
      {open && (
        <div className="category-picker-list">
          <input
            aria-label="Найти категорию"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Поиск по дереву категорий"
          />
          <button
            type="button"
            className={value ? "" : "active"}
            onClick={() => {
              onChange(undefined);
              setOpen(false);
            }}
          >
            Не указано
          </button>
          {visible.map(({ item, depth }) => {
            const hasChildren = categories.some((candidate) => candidate.parentId === item.id);
            return (
              <div
                className="category-picker-row"
                key={item.id}
                style={{ paddingLeft: depth * 18 }}
              >
                {hasChildren ? (
                  <button
                    type="button"
                    className="category-toggle"
                    aria-label={`${expanded.has(item.id) ? "Свернуть" : "Раскрыть"} ${item.name}`}
                    aria-expanded={expanded.has(item.id)}
                    onClick={() => toggle(item.id)}
                  >
                    {expanded.has(item.id) ? "−" : "+"}
                  </button>
                ) : (
                  <span className="category-toggle placeholder" aria-hidden="true" />
                )}
                <button
                  type="button"
                  className={value === item.id ? "active" : ""}
                  onClick={() => {
                    onChange(item.id);
                    setOpen(false);
                  }}
                >
                  {query.trim() ? categoryPath(categories, item.id) : item.name}
                </button>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
