import { orderCategoryTree } from "./CategoryPicker";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ImportDialog,
  MergeProductsDialog,
  NewProductDialog,
  ProductDialog,
  PublishedMergePreviewDialog,
} from "./AdminCatalogDialogs";
import type { ProductEditorSection } from "./AdminCatalogDialogs";
import "./styles/admin-catalog.css";
import { PageHeading, api, money, statusLabels } from "./adminShared";
import { WorkspaceMore } from "./WorkspaceUI";
import { AttributeManager } from "./AdminPim";
import { CollectionsV2 } from "./AdminCollectionsV2";
import { AdminProductLinks, type LinkChannel } from "./AdminProductLinks";
import {
  catalogSlug,
  categoryPath,
  channelLabels,
  normalizeCatalogQuery,
  readinessFor,
  rootCategory as findRootCategory,
  type ReadinessCode,
  type RegisterCatalogLeave,
} from "./adminCatalogLogic";
import type { Category, CategoryAttribute, Product, ReviewModerationItem } from "./adminTypes";

export function Categories({
  canEdit,
  owner,
  onError,
}: {
  canEdit: boolean;
  owner: boolean;
  onError: (value: string) => void;
}) {
  const [items, setItems] = useState<Category[]>([]);
  const [panel, setPanel] = useState<"tree" | "attributes" | "filters">("tree");
  const [name, setName] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);
  const [slug, setSlug] = useState("");
  const [parentId, setParentId] = useState("");
  const load = useCallback(
    () =>
      api<{ categories: Category[] }>("/api/v1/admin/categories")
        .then((data) => setItems(data.categories))
        .catch((error) => onError(error.message)),
    [onError],
  );
  useEffect(() => {
    void load();
  }, [load]);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const ordered = orderCategoryTree(items);
  const orderedItems = ordered.map((entry) => entry.item);
  const depth = (item: Category) => ordered.find((entry) => entry.item.id === item.id)?.depth ?? 0;
  // Only rows whose whole chain of parents is expanded are shown; the tree
  // therefore starts collapsed to the root sections.
  const visibleItems = orderedItems.filter((item) => {
    if (query.trim())
      return normalizeCatalogQuery(categoryPath(items, item.id)).includes(
        normalizeCatalogQuery(query),
      );
    let parent = item.parentId;
    while (parent) {
      if (!expanded.has(parent)) return false;
      parent = items.find((candidate) => candidate.id === parent)?.parentId ?? null;
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
  const create = async () => {
    if (creating) return;
    setCreating(true);
    try {
      await api("/api/v1/admin/categories", {
        method: "POST",
        body: JSON.stringify({
          name,
          slug,
          parentId: parentId ? Number(parentId) : null,
          sortOrder: items.length * 10,
        }),
      });
      setName("");
      setSlug("");
      setSlugEdited(false);
      setParentId("");
      load();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setCreating(false);
    }
  };
  const rename = async (item: Category) => {
    const next = window.prompt("Новое название категории", item.name);
    if (!next || next === item.name) return;
    try {
      await api(`/api/v1/admin/categories/${item.id}`, {
        method: "PATCH",
        body: JSON.stringify({ name: next }),
      });
      load();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  const remove = async (item: Category) => {
    if (!window.confirm(`Удалить категорию «${item.name}»?`)) return;
    try {
      await api(`/api/v1/admin/categories/${item.id}`, { method: "DELETE" });
      load();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  const total = (id: number, seen = new Set<number>()): number => {
    if (seen.has(id)) return 0;
    seen.add(id);
    return (
      (items.find((x) => x.id === id)?.productsCount || 0) +
      items.filter((x) => x.parentId === id).reduce((sum, x) => sum + total(x.id, seen), 0)
    );
  };
  return (
    <>
      <PageHeading title="Категории" text="Дерево товаров, характеристики и фильтры витрины" />
      <nav className="catalog-work-views" aria-label="Настройки структуры">
        {(
          [
            { id: "tree", label: "Категории" },
            { id: "attributes", label: "Характеристики" },
            { id: "filters", label: "Фильтры витрины" },
          ] as const
        ).map((x) => (
          <button
            key={x.id}
            className={panel === x.id ? "active" : ""}
            onClick={() => setPanel(x.id)}
          >
            {x.label}
          </button>
        ))}
      </nav>
      {panel === "tree" ? (
        <>
          {canEdit && (
            <div className="admin-toolbar category-create">
              <input
                aria-label="Название категории"
                value={name}
                onChange={(event) => {
                  setName(event.target.value);
                  if (!slugEdited) setSlug(catalogSlug(event.target.value));
                }}
                placeholder="Название"
              />
              <input
                aria-label="Адрес категории"
                value={slug}
                onChange={(event) => {
                  setSlugEdited(true);
                  setSlug(event.target.value.toLowerCase().replace(/[^a-z0-9-]+/g, "-"));
                }}
                placeholder="Адрес латиницей"
              />
              <select
                aria-label="Родительская категория"
                value={parentId}
                onChange={(event) => setParentId(event.target.value)}
              >
                <option value="">Корневая категория</option>
                {orderedItems.map((item) => (
                  <option
                    value={item.id}
                    key={item.id}
                  >{`${"— ".repeat(depth(item))}${item.name}`}</option>
                ))}
              </select>
              <button
                className="admin-primary"
                disabled={creating || !name.trim() || !slug.trim()}
                onClick={create}
              >
                {creating ? "Создаём…" : "Добавить"}
              </button>
            </div>
          )}
          <div className="admin-toolbar category-expand">
            <input
              aria-label="Поиск категорий"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Название или путь категории"
            />
            <button
              className="admin-action"
              onClick={() => setExpanded(new Set(items.map((item) => item.id)))}
            >
              Раскрыть всё
            </button>
            <button className="admin-action" onClick={() => setExpanded(new Set())}>
              Свернуть всё
            </button>
          </div>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>Категория</th>
                  <th>Адрес</th>
                  <th>Товары с подкатегориями</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {visibleItems.map((item) => {
                  const hasChildren = items.some((candidate) => candidate.parentId === item.id);
                  return (
                    <tr key={item.id}>
                      <td>
                        <strong style={{ paddingLeft: depth(item) * 24 }}>
                          {hasChildren ? (
                            <button
                              className="category-toggle"
                              aria-label={`${expanded.has(item.id) ? "Свернуть" : "Раскрыть"} ${item.name}`}
                              aria-expanded={expanded.has(item.id)}
                              onClick={(event) => {
                                event.stopPropagation();
                                toggle(item.id);
                              }}
                            >
                              {expanded.has(item.id) ? "−" : "+"}
                            </button>
                          ) : (
                            <span className="category-toggle placeholder" aria-hidden="true" />
                          )}
                          {item.name}
                        </strong>
                      </td>
                      <td>
                        <code>{item.slug}</code>
                      </td>
                      <td>
                        {total(item.id)}
                        {hasChildren && <small>Непосредственно: {item.productsCount}</small>}
                      </td>
                      <td>
                        {canEdit && (
                          <button className="text-button" onClick={() => void rename(item)}>
                            Изменить
                          </button>
                        )}
                        {owner && (
                          <button
                            disabled={hasChildren || total(item.id) > 0}
                            title={
                              hasChildren
                                ? "Сначала перенесите дочерние категории"
                                : total(item.id) > 0
                                  ? "Сначала перенесите товары"
                                  : "Удалить пустую категорию"
                            }
                            className="text-button danger"
                            onClick={(event) => {
                              event.stopPropagation();
                              remove(item);
                            }}
                          >
                            Удалить
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </>
      ) : owner ? (
        <AttributeManager
          key={panel}
          initialView={panel === "filters" ? "filters" : "definitions"}
          categories={items}
          onError={onError}
        />
      ) : (
        <p>Настройка характеристик доступна владельцу.</p>
      )}
    </>
  );
}

export function Collections({
  owner,
  onError,
  registerBeforeLeave,
}: {
  owner: boolean;
  onError: (value: string) => void;
  registerBeforeLeave?: RegisterCatalogLeave;
}) {
  return (
    <CollectionsV2 owner={owner} onError={onError} registerBeforeLeave={registerBeforeLeave} />
  );
}

type CatalogFilters = {
  q: string;
  status: string;
  stock: string;
  root: string;
  category: string;
  quality: string;
  sort: string;
  page: number;
  size: number;
};
const defaultFilters: CatalogFilters = {
  q: "",
  status: "all",
  stock: "all",
  root: "all",
  category: "all",
  quality: "all",
  sort: "updated",
  page: 1,
  size: 50,
};
function readCatalogFilters(): CatalogFilters {
  const p = new URLSearchParams(location.search);
  return {
    q: p.get("q") || "",
    status: ["draft", "published", "archived"].includes(p.get("status") || "")
      ? p.get("status")!
      : "all",
    stock: ["positive", "nonpositive"].includes(p.get("stock") || "") ? p.get("stock")! : "all",
    root: p.get("root") || "all",
    category: p.get("category") || "all",
    quality: p.get("quality") || "all",
    sort: ["name", "price", "stock", "tasks", "updated"].includes(p.get("sort") || "")
      ? p.get("sort")!
      : "updated",
    page: Math.max(1, Number(p.get("page")) || 1),
    size: [25, 50, 100].includes(Number(p.get("size"))) ? Number(p.get("size")) : 50,
  };
}
function catalogURL(patch: Record<string, string | null>) {
  const url = new URL(location.href);
  url.searchParams.set("section", "products");
  for (const [key, value] of Object.entries(patch)) {
    if (value === null) url.searchParams.delete(key);
    else url.searchParams.set(key, value);
  }
  return url.pathname + url.search;
}
export function Products({
  can,
  onError,
  registerBeforeLeave,
}: {
  can: (permission: string) => boolean;
  onError: (value: string) => void;
  registerBeforeLeave?: (fn: () => Promise<boolean>) => () => void;
}) {
  const [view, setView] = useState<"catalog" | "reviews" | LinkChannel>("catalog");
  const [items, setItems] = useState<Product[]>([]);
  const [filters, setFilters] = useState(readCatalogFilters);
  const [categories, setCategories] = useState<Category[]>([]);
  const [schemas, setSchemas] = useState<Record<number, CategoryAttribute[]>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<number[]>([]);
  const [confirmation, setConfirmation] = useState<"publish" | "delete" | null>(null);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{
    published: number[];
    blocked: Array<{ productId: number; name: string; reason: string }>;
    errors: Array<{ productId: number; message: string }>;
  } | null>(null);
  const [editingId, setEditingId] = useState(
    () => Number(new URLSearchParams(location.search).get("product")) || null,
  );
  const [editorSection, setEditorSection] = useState<ProductEditorSection>(
    () =>
      (["main", "attributes", "care", "variants", "relationships", "photos"].includes(
        new URLSearchParams(location.search).get("tab") || "",
      )
        ? new URLSearchParams(location.search).get("tab")
        : "main") as ProductEditorSection,
  );
  const [creating, setCreating] = useState(false);
  const [importing, setImporting] = useState(false);
  const [merging, setMerging] = useState<Product[] | null>(null);
  const [publishedMerge, setPublishedMerge] = useState<Product[] | null>(null);
  const [reviews, setReviews] = useState<ReviewModerationItem[]>([]);
  const editorDirty = useRef(false);
  const editorURL = useRef(location.href);
  const setEditorDirty = useCallback((value: boolean) => {
    editorDirty.current = value;
  }, []);
  const loadSequence = useRef(0);
  const mutationRevision = useRef(0);
  const locallyUpdated = useRef(new Map<number, number>());
  const reload = useCallback(async () => {
    const request = ++loadSequence.current;
    const revision = mutationRevision.current;
    setLoading(true);
    setError("");
    try {
      const data = await api<{ products: Product[] }>("/api/v1/admin/products");
      if (request !== loadSequence.current) return;
      // A slow list response may predate a successful create/save. Keep those
      // local results while still loading the rest of the server catalogue.
      setItems((current) => {
        const updated = current.filter(
          (item) => (locallyUpdated.current.get(item.id) || 0) > revision,
        );
        const ids = new Set(updated.map((item) => item.id));
        return [...updated, ...(data.products || []).filter((item) => !ids.has(item.id))];
      });
      setSelected([]);
    } catch (error) {
      if (request === loadSequence.current) setError((error as Error).message);
    } finally {
      if (request === loadSequence.current) setLoading(false);
    }
  }, []);
  useEffect(() => {
    void reload();
    api<{ categories: Category[] }>("/api/v1/admin/categories")
      .then((data) => setCategories(data.categories || []))
      .catch((error) => onError(error.message));
  }, [reload, onError]);
  useEffect(() => {
    const ids = [...new Set(items.map((x) => x.categoryId).filter((x): x is number => !!x))].filter(
      (id) => schemas[id] === undefined,
    );
    if (!ids.length) return;
    let active = true;
    Promise.all(
      ids.map(
        async (id) =>
          [
            id,
            (
              await api<{ attributes: CategoryAttribute[] }>(
                `/api/v1/admin/categories/${id}/attributes`,
              )
            ).attributes || [],
          ] as const,
      ),
    )
      .then((entries) => {
        if (active) setSchemas((current) => ({ ...current, ...Object.fromEntries(entries) }));
      })
      .catch((error) => onError(error.message));
    return () => {
      active = false;
    };
  }, [items, schemas, onError]);
  useEffect(() => {
    if (view === "reviews")
      api<{ reviews: ReviewModerationItem[] }>("/api/v1/admin/reviews")
        .then((data) => setReviews(data.reviews || []))
        .catch((error) => onError(error.message));
  }, [view, onError]);
  useEffect(
    () =>
      registerBeforeLeave?.(async () => {
        const allowed =
          !editorDirty.current ||
          window.confirm(
            "В карточке есть несохранённые правки. Отказаться от них и перейти в другой раздел?",
          );
        if (!allowed && location.href !== editorURL.current)
          history.pushState(null, "", editorURL.current);
        return allowed;
      }),
    [registerBeforeLeave],
  );
  useEffect(() => {
    const read = () => {
      if (new URLSearchParams(location.search).get("section") !== "products") return;
      if (editorDirty.current && !window.confirm("Отказаться от несохранённых правок карточки?")) {
        history.pushState(null, "", editorURL.current);
        return;
      }
      setFilters(readCatalogFilters());
      setSelected([]);
      setEditingId(Number(new URLSearchParams(location.search).get("product")) || null);
      setEditorSection(
        (new URLSearchParams(location.search).get("tab") || "main") as ProductEditorSection,
      );
      editorDirty.current = false;
      editorURL.current = location.href;
    };
    window.addEventListener("popstate", read);
    return () => window.removeEventListener("popstate", read);
  }, []);
  const changeFilters = (patch: Partial<CatalogFilters>) => {
    const next = { ...filters, ...patch, page: patch.page ?? 1 };
    setFilters(next);
    setSelected([]);
    setConfirmation(null);
    setResult(null);
    const params: Record<string, string | null> = {};
    for (const [key, value] of Object.entries(next))
      params[key] =
        String(value) === String(defaultFilters[key as keyof CatalogFilters])
          ? null
          : String(value);
    history.replaceState(null, "", catalogURL(params));
  };
  const reset = () => changeFilters(defaultFilters);
  const readiness = useMemo(
    () =>
      new Map(
        items.map((x) => [
          x.id,
          readinessFor(x, categories, x.categoryId ? schemas[x.categoryId] || [] : []),
        ]),
      ),
    [items, categories, schemas],
  );
  const scoped = useMemo(
    () =>
      items.filter((x) => {
        if (
          (filters.status !== "all" && x.status !== filters.status) ||
          (filters.stock === "positive" && (x.totalStock ?? x.stock) <= 0) ||
          (filters.stock === "nonpositive" && (x.totalStock ?? x.stock) > 0)
        )
          return false;
        if (filters.root === "none" && x.categoryId) return false;
        if (
          !["all", "none"].includes(filters.root) &&
          String(findRootCategory(categories, x.categoryId)?.id) !== filters.root
        )
          return false;
        if (filters.category !== "all") {
          let current = categories.find((c) => c.id === x.categoryId);
          const seen = new Set<number>();
          while (current && String(current.id) !== filters.category && !seen.has(current.id)) {
            seen.add(current.id);
            current = categories.find((c) => c.id === current?.parentId);
          }
          if (!current || String(current.id) !== filters.category) return false;
        }
        return normalizeCatalogQuery(
          `${x.name} ${x.latinName} ${x.sku} ${x.sabyCode} ${(x.externalIds || []).map((e) => e.externalId).join(" ")}`,
        ).includes(normalizeCatalogQuery(filters.q));
      }),
    [items, filters, categories],
  );
  const filtered = useMemo(
    () =>
      scoped
        .filter((x) =>
          filters.quality === "all" || filters.quality === "attention"
            ? filters.quality !== "attention" || !!readiness.get(x.id)?.tasks.length
            : readiness.get(x.id)?.codes.includes(filters.quality as ReadinessCode),
        )
        .sort((a, b) =>
          filters.sort === "name"
            ? a.name.localeCompare(b.name, "ru")
            : filters.sort === "price"
              ? (a.minPrice ?? a.price) - (b.minPrice ?? b.price)
              : filters.sort === "stock"
                ? (b.totalStock ?? b.stock) - (a.totalStock ?? a.stock)
                : filters.sort === "tasks"
                  ? (readiness.get(b.id)?.tasks.length || 0) -
                    (readiness.get(a.id)?.tasks.length || 0)
                  : new Date(b.updatedAt || 0).getTime() - new Date(a.updatedAt || 0).getTime(),
        ),
    [scoped, filters.quality, filters.sort, readiness],
  );
  const pageCount = Math.max(1, Math.ceil(filtered.length / filters.size)),
    page = Math.min(filters.page, pageCount),
    pageItems = filtered.slice((page - 1) * filters.size, page * filters.size);
  const selectedProducts = pageItems.filter((x) => selected.includes(x.id));
  const selectedAreDrafts =
    selectedProducts.length > 0 && selectedProducts.every((x) => x.status === "draft");
  const selectedArePublishedPlants =
    selectedProducts.length >= 2 &&
    selectedProducts.every((x) => x.status === "published" && x.catalogSection === "plants");
  const editing = items.find((x) => x.id === editingId);
  const open = (productId: number, section: ProductEditorSection = "main") => {
    setEditingId(productId);
    setEditorSection(section);
    history.pushState(null, "", catalogURL({ product: String(productId), tab: section }));
    editorURL.current = location.href;
  };
  const close = () => {
    setEditingId(null);
    editorDirty.current = false;
    history.replaceState(null, "", catalogURL({ product: null, tab: null }));
    editorURL.current = location.href;
  };
  const changeSection = (section: ProductEditorSection) => {
    setEditorSection(section);
    history.replaceState(null, "", catalogURL({ tab: section }));
    editorURL.current = location.href;
  };
  const publish = async () => {
    if (!selectedAreDrafts || busy) return;
    setBusy(true);
    const published: number[] = [],
      blocked: Array<{ productId: number; name: string; reason: string }> = [],
      errors: Array<{ productId: number; message: string }> = [];
    try {
      for (let i = 0; i < selectedProducts.length; i += 25) {
        const ids = selectedProducts.slice(i, i + 25).map((x) => x.id);
        try {
          const data = await api<{ published: number[]; blocked: typeof blocked }>(
            "/api/v1/admin/products/publish",
            { method: "POST", body: JSON.stringify({ productIds: ids }) },
          );
          published.push(...data.published);
          blocked.push(...data.blocked);
        } catch (error) {
          errors.push(
            ...ids.map((productId) => ({ productId, message: (error as Error).message })),
          );
        }
      }
      setResult({ published, blocked, errors });
      setConfirmation(null);
      await reload();
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    if (!selectedAreDrafts || busy) return;
    setBusy(true);
    try {
      await api("/api/v1/admin/products", {
        method: "DELETE",
        body: JSON.stringify({ productIds: selectedProducts.map((x) => x.id) }),
      });
      setConfirmation(null);
      await reload();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const moderate = async (id: number, status: "published" | "rejected") => {
    try {
      await api(`/api/v1/admin/reviews/${id}`, {
        method: "PATCH",
        body: JSON.stringify({ status }),
      });
      setReviews((current) => current.map((x) => (x.id === id ? { ...x, status } : x)));
    } catch (error) {
      onError((error as Error).message);
    }
  };
  const count = (code: ReadinessCode) =>
    scoped.filter((x) => readiness.get(x.id)?.codes.includes(code)).length;
  const stockOf = (x: Product) => x.totalStock ?? x.stock;
  const base = { status: "all", stock: "all", quality: "all" };
  const statusTabs = [
    {
      id: "all",
      label: "Все",
      filters: base,
      count: items.length,
      active: filters.status === "all" && filters.stock === "all" && filters.quality === "all",
    },
    {
      id: "published",
      label: "На сайте",
      filters: { ...base, status: "published" },
      count: items.filter((x) => x.status === "published").length,
      active:
        filters.status === "published" && filters.stock === "all" && filters.quality === "all",
    },
    {
      id: "draft",
      label: "Черновики",
      filters: { ...base, status: "draft" },
      count: items.filter((x) => x.status === "draft").length,
      active: filters.status === "draft" && filters.stock === "all" && filters.quality === "all",
    },
    {
      id: "attention",
      label: "Нужно доделать",
      filters: { ...base, quality: "attention" },
      count: items.filter((x) => readiness.get(x.id)?.tasks.length).length,
      active: filters.quality === "attention",
    },
    {
      id: "stock",
      label: "Нет в наличии",
      filters: { ...base, stock: "nonpositive" },
      count: items.filter((x) => stockOf(x) <= 0).length,
      active: filters.stock === "nonpositive",
    },
    {
      id: "archived",
      label: "Архив",
      filters: { ...base, status: "archived" },
      count: items.filter((x) => x.status === "archived").length,
      active: filters.status === "archived",
    },
  ];
  // Фильтры из панели: раздел, категория, сортировка и «что доделать».
  const appliedFilters =
    (["root", "category", "sort"] as const).filter((k) => filters[k] !== defaultFilters[k]).length +
    (filters.quality !== "all" && filters.quality !== "attention" ? 1 : 0);
  return (
    <div className="catalog-workspace">
      <div className="catalog-page-heading">
        <h1>
          {view === "catalog"
            ? "Список товаров"
            : view === "reviews"
              ? "Отзывы"
              : "Связи с каналами"}
        </h1>
        <div>
          {view !== "catalog" && (
            <button
              type="button"
              className="admin-action"
              onClick={() => {
                setView("catalog");
                setSelected([]);
                setConfirmation(null);
              }}
            >
              К списку товаров
            </button>
          )}
          {view === "catalog" && can("products.manage") && (
            <>
              <button type="button" className="admin-action" onClick={() => setImporting(true)}>
                Импорт из СБИС
              </button>
              <button type="button" className="admin-primary" onClick={() => setCreating(true)}>
                Добавить товар
              </button>
            </>
          )}
          {/* Редкие разделы каталога — в «Ещё», чтобы над списком не было
            трёх этажей вкладок. */}
          <WorkspaceMore>
            <button
              type="button"
              onClick={() => {
                setView("reviews");
                setSelected([]);
                setConfirmation(null);
              }}
            >
              Отзывы
            </button>
            <button
              type="button"
              onClick={() => {
                setView("saby");
                setSelected([]);
                setConfirmation(null);
              }}
            >
              Связи с каналами
            </button>
            <a href="/admin?section=categories">Категории</a>
            <a href="/admin?section=collections">Подборки</a>
          </WorkspaceMore>
        </div>
      </div>
      {["saby", "wb", "ozon", "avito"].includes(view) && (
        <nav className="catalog-channel-switch" aria-label="Канал">
          {(
            [
              { id: "saby", label: "СБИС" },
              { id: "wb", label: "Wildberries" },
              { id: "ozon", label: "Ozon" },
              { id: "avito", label: "Авито" },
            ] as const
          ).map((x) => (
            <button
              type="button"
              key={x.id}
              aria-current={view === x.id ? "page" : undefined}
              className={view === x.id ? "active" : ""}
              onClick={() => setView(x.id)}
            >
              {x.label}
            </button>
          ))}
        </nav>
      )}
      {view === "catalog" ? (
        <>
          <nav className="catalog-status-tabs" aria-label="Состояние товаров">
            {statusTabs.map((tab) => (
              <button
                type="button"
                key={tab.id}
                aria-current={tab.active ? "page" : undefined}
                onClick={() => changeFilters(tab.filters)}
              >
                {tab.label} <span>{tab.count}</span>
              </button>
            ))}
          </nav>
          <div className="catalog-toolbar-search">
            <label>
              <svg
                viewBox="0 0 24 24"
                width="16"
                height="16"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                aria-hidden="true"
              >
                <circle cx="11" cy="11" r="7" />
                <path d="m20 20-3.5-3.5" />
              </svg>
              <input
                aria-label="Поиск товаров"
                value={filters.q}
                placeholder="Название, артикул, код СБИС"
                onChange={(e) => changeFilters({ q: e.target.value })}
              />
            </label>
            <details className="catalog-filters">
              <summary>Фильтры{appliedFilters > 0 ? ` · ${appliedFilters}` : ""}</summary>
              <div className="catalog-filters-panel">
                <div className="catalog-filter-fields">
                  <label>
                    Раздел
                    <select
                      aria-label="Раздел каталога"
                      value={filters.root}
                      onChange={(e) => changeFilters({ root: e.target.value, category: "all" })}
                    >
                      <option value="all">Все разделы</option>
                      {categories
                        .filter((x) => x.parentId === null)
                        .sort((a, b) => a.sortOrder - b.sortOrder)
                        .map((x) => (
                          <option value={x.id} key={x.id}>
                            {x.name}
                          </option>
                        ))}
                      <option value="none">Без категории</option>
                    </select>
                  </label>
                  <label>
                    Категория
                    <select
                      aria-label="Подкатегория"
                      disabled={filters.root === "none"}
                      value={filters.category}
                      onChange={(e) => changeFilters({ category: e.target.value })}
                    >
                      <option value="all">Все категории</option>
                      {orderCategoryTree(categories)
                        .filter(
                          ({ item }) =>
                            filters.root === "all" ||
                            String(findRootCategory(categories, item.id)?.id) === filters.root,
                        )
                        .map(({ item }) => (
                          <option key={item.id} value={item.id}>
                            {categoryPath(categories, item.id)}
                          </option>
                        ))}
                    </select>
                  </label>
                  <label>
                    Сортировка
                    <select
                      aria-label="Сортировка"
                      value={filters.sort}
                      onChange={(e) => changeFilters({ sort: e.target.value })}
                    >
                      <option value="updated">Последние изменения</option>
                      <option value="name">По названию</option>
                      <option value="price">По цене ↑</option>
                      <option value="stock">По остатку ↓</option>
                      <option value="tasks">Сначала с замечаниями</option>
                    </select>
                  </label>
                </div>
                <span className="catalog-filters-caption">Что доделать в карточке</span>
                <div className="admin-readiness-counters">
                  {(
                    [
                      { code: "no_photo", label: "Без фото" },
                      { code: "no_description", label: "Без описания" },
                      { code: "stage1_missing", label: "Характеристики" },
                      { code: "care_missing", label: "Без ухода" },
                      { code: "ready", label: "Черновики без замечаний" },
                    ] as const
                  ).map((x) => (
                    <button
                      type="button"
                      aria-pressed={filters.quality === x.code}
                      key={x.code}
                      onClick={() =>
                        changeFilters({ quality: filters.quality === x.code ? "all" : x.code })
                      }
                    >
                      {x.label} <b>{count(x.code)}</b>
                    </button>
                  ))}
                </div>
              </div>
            </details>
            {(appliedFilters > 0 || filters.q) && (
              <button type="button" className="text-button" onClick={reset}>
                Сбросить фильтры
              </button>
            )}
          </div>
          {selectedProducts.length > 0 && (
            <section className="catalog-selection" aria-label="Выбранные товары">
              <details>
                <summary>Выбрано: {selectedProducts.length} · показать товары</summary>
                <ul>
                  {selectedProducts.map((x) => (
                    <li key={x.id}>
                      {x.name} · артикул {x.sku}
                    </li>
                  ))}
                </ul>
              </details>
              <div>
                <button
                  onClick={() => {
                    setSelected([]);
                    setConfirmation(null);
                  }}
                >
                  Снять всё
                </button>
                {selectedAreDrafts && can("products.edit") && (
                  <button className="admin-primary" onClick={() => setConfirmation("publish")}>
                    Опубликовать
                  </button>
                )}
                {selectedAreDrafts && can("catalog.delete") && (
                  <button onClick={() => setConfirmation("delete")}>Удалить черновики</button>
                )}
                {selectedAreDrafts && selectedProducts.length >= 2 && can("products.manage") && (
                  <button onClick={() => setMerging(selectedProducts)}>Объединить размеры</button>
                )}
                {selectedArePublishedPlants && can("products.manage") && (
                  <button onClick={() => setPublishedMerge(selectedProducts)}>
                    Проверить объединение
                  </button>
                )}
              </div>
              <small>
                Выбор относится только к текущей странице. Поиск, фильтры и переход по страницам
                снимают выделение.
              </small>
            </section>
          )}
          {confirmation && selectedProducts.length > 0 && (
            <div
              className="catalog-bulk-confirm"
              role="region"
              aria-label="Подтверждение массового действия"
            >
              <strong>
                {confirmation === "publish" ? "Публикация" : "Удаление черновиков"}:{" "}
                {selectedProducts.length}
              </strong>
              <ul>
                {selectedProducts.map((x) => (
                  <li key={x.id}>
                    {x.name} · артикул {x.sku}
                  </li>
                ))}
              </ul>
              <p>
                {confirmation === "publish"
                  ? "Сервер проверит каждую карточку. Пропуски и ошибки будут перечислены отдельно."
                  : "Удаляются только непроданные черновики. Это действие нельзя отменить."}
              </p>
              <button disabled={busy} onClick={() => setConfirmation(null)}>
                Отмена
              </button>
              <button
                className={confirmation === "publish" ? "admin-primary" : "admin-danger"}
                disabled={busy}
                onClick={() => void (confirmation === "publish" ? publish() : remove())}
              >
                {busy
                  ? "Выполняем…"
                  : confirmation === "publish"
                    ? `Подтвердить публикацию (${selectedProducts.length})`
                    : `Подтвердить удаление (${selectedProducts.length})`}
              </button>
            </div>
          )}
          {result && (
            <div className="admin-bulk-result" role="status">
              <strong>
                Успешно: {result.published.length} · Пропущено: {result.blocked.length} · Ошибка:{" "}
                {result.errors.length}
              </strong>
              {result.blocked.map((x) => (
                <p key={x.productId}>
                  {x.name || `Товар ${x.productId}`}: {x.reason}{" "}
                  <button
                    type="button"
                    className="text-button"
                    onClick={() => open(x.productId, "attributes")}
                  >
                    Исправить
                  </button>
                </p>
              ))}
              {result.errors.map((x) => (
                <p key={x.productId}>
                  {items.find((p) => p.id === x.productId)?.name || x.productId}: {x.message}
                </p>
              ))}
            </div>
          )}
          {loading ? (
            <p role="status" className="catalog-empty">
              Загружаем товары…
            </p>
          ) : error ? (
            <div className="catalog-empty" role="alert">
              <h3>Не удалось загрузить каталог</h3>
              <p>{error}</p>
              <button className="admin-action" onClick={() => void reload()}>
                Повторить
              </button>
            </div>
          ) : !filtered.length ? (
            <div className="catalog-empty">
              <h3>Товары не найдены</h3>
              <p>
                {filters.q
                  ? `По запросу «${filters.q.trim()}» с выбранными фильтрами ничего нет.`
                  : "Попробуйте другие фильтры или добавьте первый товар."}
              </p>
              <button className="admin-primary" onClick={reset}>
                Сбросить фильтры
              </button>
            </div>
          ) : (
            <>
              <div className="catalog-products-wrap">
                <table className="admin-table products">
                  <thead>
                    <tr>
                      <th>
                        <input
                          type="checkbox"
                          aria-label="Выбрать товары на странице"
                          checked={
                            pageItems.length > 0 && pageItems.every((x) => selected.includes(x.id))
                          }
                          onChange={(e) => {
                            setSelected(e.target.checked ? pageItems.map((x) => x.id) : []);
                            setConfirmation(null);
                          }}
                        />
                      </th>
                      <th>Фото</th>
                      <th>Товар</th>
                      <th>Статус</th>
                      <th>Цена</th>
                      <th>В наличии</th>
                      <th>Каналы</th>
                      <th>
                        <span className="chat-sr-only">Действия</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageItems.map((product) => {
                      const tasks = readiness.get(product.id)?.tasks || [];
                      return (
                        <tr
                          key={product.id}
                          className={selected.includes(product.id) ? "selected" : ""}
                        >
                          <td className="catalog-check">
                            <input
                              type="checkbox"
                              aria-label={`Выбрать ${product.name}`}
                              checked={selected.includes(product.id)}
                              onChange={(e) => {
                                setSelected((current) =>
                                  e.target.checked
                                    ? [...current, product.id]
                                    : current.filter((id) => id !== product.id),
                                );
                                setConfirmation(null);
                              }}
                            />
                          </td>
                          <td className="catalog-photo-cell">
                            {product.image ? (
                              <img loading="lazy" src={product.image} alt="" />
                            ) : (
                              <span className="catalog-photo-empty" aria-label="Нет фото" />
                            )}
                          </td>
                          <td className="catalog-product-cell">
                            {can("products.edit") ? (
                              <a
                                className="catalog-product-name"
                                href={catalogURL({ product: String(product.id), tab: "main" })}
                                onClick={(e) => {
                                  e.preventDefault();
                                  open(product.id);
                                }}
                              >
                                {product.name}
                              </a>
                            ) : (
                              <strong className="catalog-product-name">{product.name}</strong>
                            )}
                            <small>
                              Артикул {product.sku} ·{" "}
                              {product.variantCount && product.variantCount > 1
                                ? `${product.variantCount} размеров`
                                : product.variantLabel}
                            </small>
                            {tasks.length > 0 && (
                              <button
                                type="button"
                                className="catalog-task"
                                disabled={!can("products.edit")}
                                onClick={() => open(product.id, tasks[0].section)}
                              >
                                {tasks[0].label}
                                {tasks.length > 1 ? ` и ещё ${tasks.length - 1}` : ""}
                              </button>
                            )}
                          </td>
                          <td data-label="Статус">
                            <span className={`admin-pill ${product.status}`}>
                              {statusLabels[product.status] || product.status}
                            </span>
                          </td>
                          <td data-label="Цена" className="catalog-price-cell">
                            {product.minPrice !== undefined &&
                            product.maxPrice !== undefined &&
                            product.maxPrice > product.minPrice
                              ? `${money.format(product.minPrice)}–${money.format(product.maxPrice)}`
                              : money.format(product.minPrice ?? product.price)}
                          </td>
                          <td
                            data-label="В наличии"
                            data-empty={(product.totalStock ?? product.stock) <= 0 || undefined}
                          >
                            {(product.totalStock ?? product.stock) > 0
                              ? `${product.totalStock ?? product.stock} шт.`
                              : "0 шт."}
                          </td>
                          <td data-label="Каналы" className="catalog-channels-cell">
                            {[
                              ...new Set(
                                (product.externalIds || [])
                                  .map((x) => x.provider)
                                  .filter((x) => x !== "ficusin"),
                              ),
                            ]
                              .map((x) => channelLabels[x] || "Другая система")
                              .join(", ") || "—"}
                          </td>
                          <td className="catalog-actions-cell">
                            {can("products.edit") && (
                              <button
                                type="button"
                                className="catalog-icon-button"
                                aria-label={`Изменить ${product.name}`}
                                title="Изменить"
                                onClick={() => open(product.id)}
                              >
                                <svg
                                  viewBox="0 0 24 24"
                                  width="16"
                                  height="16"
                                  fill="none"
                                  stroke="currentColor"
                                  strokeWidth="2"
                                  aria-hidden="true"
                                >
                                  <path d="M4 20h4L19 9l-4-4L4 16z" />
                                </svg>
                              </button>
                            )}
                            {product.status === "published" && (
                              <a
                                className="catalog-icon-button"
                                href={`/product/${product.productCode || product.slug}`}
                                target="_blank"
                                rel="noreferrer"
                                aria-label="На сайте ↗"
                                title="Открыть на сайте"
                              >
                                <svg
                                  viewBox="0 0 24 24"
                                  width="16"
                                  height="16"
                                  fill="none"
                                  stroke="currentColor"
                                  strokeWidth="2"
                                  aria-hidden="true"
                                >
                                  <path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
                                </svg>
                              </a>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
              <div className="catalog-pagination">
                <label>
                  На странице
                  <select
                    aria-label="Товаров на странице"
                    value={filters.size}
                    onChange={(e) => changeFilters({ size: Number(e.target.value) })}
                  >
                    <option>25</option>
                    <option>50</option>
                    <option>100</option>
                  </select>
                </label>
                <span>
                  Страница {page} из {pageCount}
                </span>
                <div>
                  <button disabled={page <= 1} onClick={() => changeFilters({ page: page - 1 })}>
                    ← Назад
                  </button>
                  <button
                    disabled={page >= pageCount}
                    onClick={() => changeFilters({ page: page + 1 })}
                  >
                    Далее →
                  </button>
                </div>
              </div>
            </>
          )}
        </>
      ) : view === "reviews" ? (
        <section className="admin-block review-moderation">
          <div className="admin-block-heading">
            <div>
              <p className="eyebrow">Обратная связь</p>
              <h2>Все отзывы</h2>
            </div>
            <span className="admin-pill">{reviews.length}</span>
          </div>
          {reviews.length ? (
            <div className="admin-table-wrap">
              <table className="admin-table">
                <thead>
                  <tr>
                    <th>Товар / покупатель</th>
                    <th>Оценка</th>
                    <th>Отзыв и медиа</th>
                    <th>Статус</th>
                  </tr>
                </thead>
                <tbody>
                  {reviews.map((review) => (
                    <tr key={review.id}>
                      <td>
                        <strong>{review.product}</strong>
                        <small>
                          {review.author} · {new Date(review.createdAt).toLocaleDateString("ru-RU")}
                        </small>
                      </td>
                      <td>
                        <span className="review-stars">
                          {"★".repeat(review.rating)}
                          {"☆".repeat(5 - review.rating)}
                        </span>
                      </td>
                      <td>
                        <p>{review.text}</p>
                        {review.media?.length > 0 && (
                          <div className="admin-review-media">
                            {review.media.map((media) =>
                              media.contentType.startsWith("video/") ? (
                                <video
                                  key={media.url}
                                  src={media.url}
                                  controls
                                  preload="metadata"
                                />
                              ) : (
                                <img key={media.url} src={media.url} alt="Вложение к отзыву" />
                              ),
                            )}
                          </div>
                        )}
                      </td>
                      <td>
                        <div className="admin-review-actions">
                          <span className={`admin-pill ${review.status}`}>
                            {review.status === "published" ? "Опубликован" : "Скрыт"}
                          </span>
                          {review.status === "published" ? (
                            <button
                              disabled={!can("products.edit")}
                              onClick={() => void moderate(review.id, "rejected")}
                            >
                              Скрыть
                            </button>
                          ) : (
                            <button
                              className="admin-primary"
                              disabled={!can("products.edit")}
                              onClick={() => void moderate(review.id, "published")}
                            >
                              Вернуть на сайт
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="admin-hint">Отзывов пока нет.</p>
          )}
        </section>
      ) : (
        <AdminProductLinks
          channel={view}
          canOpen={can("products.edit")}
          canManageAvito={can("integrations.edit")}
          canLinkSaby={can("products.manage")}
          onOpen={(id) => open(id, "relationships")}
          onError={onError}
        />
      )}
      {editing && (
        <ProductDialog
          key={editing.id}
          owner={can("products.manage")}
          product={editing}
          initialSection={editorSection}
          onSectionChange={changeSection}
          onDirtyChange={setEditorDirty}
          hasNext={filtered.some((x) => x.id !== editing.id && readiness.get(x.id)?.tasks.length)}
          onClose={close}
          onSaved={(product, next) => {
            locallyUpdated.current.set(product.id, ++mutationRevision.current);
            setItems((current) => current.map((x) => (x.id === product.id ? product : x)));
            if (next) {
              const index = filtered.findIndex((x) => x.id === product.id);
              const ordered = [...filtered.slice(index + 1), ...filtered.slice(0, index)];
              const target = ordered.find((x) => readiness.get(x.id)?.tasks.length);
              if (target) {
                editorDirty.current = false;
                open(target.id);
              }
            }
          }}
          onError={onError}
        />
      )}
      {!loading && editingId && !editing && (
        <div className="catalog-empty" role="alert">
          Товар не найден или недоступен. <button onClick={close}>Вернуться к списку</button>
        </div>
      )}
      {creating && (
        <NewProductDialog
          onClose={() => setCreating(false)}
          onCreated={(product) => {
            locallyUpdated.current.set(product.id, ++mutationRevision.current);
            setCreating(false);
            setItems((current) => [product, ...current]);
            open(product.id, "variants");
          }}
          onError={onError}
        />
      )}
      {importing && (
        <ImportDialog
          onClose={() => setImporting(false)}
          onImported={() => {
            setImporting(false);
            void reload();
          }}
          onError={onError}
        />
      )}
      {merging && (
        <MergeProductsDialog
          products={merging}
          onClose={() => setMerging(null)}
          onMerged={() => {
            setMerging(null);
            void reload();
          }}
          onError={onError}
        />
      )}
      {publishedMerge && (
        <PublishedMergePreviewDialog
          products={publishedMerge}
          onClose={() => setPublishedMerge(null)}
          onMerged={() => {
            setPublishedMerge(null);
            void reload();
          }}
          onError={onError}
        />
      )}
    </div>
  );
}
