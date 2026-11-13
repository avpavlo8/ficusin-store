import { useId, useRef, useState } from "react";
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
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  const choose = (categoryId?: number) => {
    onChange(categoryId);
    setQuery("");
    input.current?.focus();
    setOpen(false);
  };
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const ordered = orderCategoryTree(categories);
  const searchQuery = normalizeCatalogQuery(query);
  const highlightMatch = (label: string) => {
    const index = label.toLocaleLowerCase("ru").indexOf(searchQuery);
    if (!searchQuery || index < 0) return label;
    return (
      <>
        {label.slice(0, index)}
        <mark>{label.slice(index, index + searchQuery.length)}</mark>
        {label.slice(index + searchQuery.length)}
      </>
    );
  };
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
    <div
      className="category-picker"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOpen(false);
      }}
    >
      <input
        ref={input}
        className="category-picker-search"
        role="combobox"
        aria-label="Найти категорию"
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        value={open ? query : value == null ? "" : categoryPath(categories, value)}
        onFocus={() => setOpen(true)}
        onClick={() => setOpen(true)}
        onChange={(event) => {
          setQuery(event.target.value);
          setOpen(true);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault();
            setQuery("");
            setOpen(false);
          }
          if (event.key === "ArrowDown") {
            event.preventDefault();
            if (!open) setOpen(true);
            else
              event.currentTarget.parentElement
                ?.querySelector<HTMLButtonElement>(".category-picker-list button")
                ?.focus();
          }
        }}
        placeholder="Найти категорию"
        autoComplete="off"
      />
      {open && (
        <div
          className="category-picker-list"
          id={listId}
          aria-label="Варианты категорий"
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              input.current?.focus();
              setQuery("");
              setOpen(false);
              return;
            }
            if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
            event.preventDefault();
            const buttons = Array.from(
              event.currentTarget.querySelectorAll<HTMLButtonElement>("button"),
            );
            const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
            const next =
              event.key === "Home"
                ? 0
                : event.key === "End"
                  ? buttons.length - 1
                  : Math.max(
                      0,
                      Math.min(buttons.length - 1, index + (event.key === "ArrowDown" ? 1 : -1)),
                    );
            buttons[next]?.focus();
          }}
        >
          <button
            type="button"
            onMouseDown={(event) => event.preventDefault()}
            className={value ? "" : "active"}
            aria-pressed={!value}
            onClick={() => choose(undefined)}
          >
            Не указано
          </button>
          {searchQuery && !visible.length && (
            <p className="category-picker-empty" role="status">
              Категории не найдены
            </p>
          )}
          {visible.map(({ item, depth }) => {
            const hasChildren = categories.some((candidate) => candidate.parentId === item.id);
            return (
              <div
                className="category-picker-row"
                key={item.id}
                style={{ paddingLeft: query.trim() ? 0 : depth * 18 }}
              >
                {!query.trim() &&
                  (hasChildren ? (
                    <button
                      type="button"
                      onMouseDown={(event) => event.preventDefault()}
                      className="category-toggle"
                      aria-label={`${expanded.has(item.id) ? "Свернуть" : "Раскрыть"} ${item.name}`}
                      aria-expanded={expanded.has(item.id)}
                      onClick={() => toggle(item.id)}
                    >
                      {expanded.has(item.id) ? "−" : "+"}
                    </button>
                  ) : (
                    <span className="category-toggle placeholder" aria-hidden="true" />
                  ))}
                <button
                  type="button"
                  onMouseDown={(event) => event.preventDefault()}
                  className={`${searchQuery ? "category-picker-match" : ""}${value === item.id ? " active" : ""}`}
                  aria-pressed={value === item.id}
                  onClick={() => choose(item.id)}
                >
                  <span>
                    {searchQuery ? highlightMatch(categoryPath(categories, item.id)) : item.name}
                  </span>
                  {value === item.id && (
                    <svg
                      width="18"
                      height="18"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      aria-hidden="true"
                    >
                      <path d="m5 12 4 4L19 6" />
                    </svg>
                  )}
                </button>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
