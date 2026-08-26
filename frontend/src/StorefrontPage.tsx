import { useEffect, useMemo, useState } from "react";
import CheckoutHost from "./CheckoutHost";
import { StoreHeader, type HeaderMenuItem } from "./StoreHeader";
import { CollectionStrip } from "./Collections";
import NotFoundPage from "./NotFoundPage";
import { searchProducts } from "./lib/search";
import { useSharedCart } from "./lib/cart";
import { track } from "./lib/analytics";
import { attributeLabel, attributeValue } from "./product/types";

type FilterDisplayMode = "select" | "chips" | "range";
type FilterDataType = "text" | "number" | "boolean" | "enum" | "multi_enum";
type FilterAttribute = {
  code: string;
  name: string;
  unit?: string;
  value: string | number | boolean | string[] | number[];
  filterable: boolean;
  badge: boolean;
  // Newer catalogue responses expose these directly from catalog_filters and
  // attribute_definitions. Fallback inference keeps older deployments usable.
  displayMode?: FilterDisplayMode;
  dataType?: FilterDataType;
};

type Product = {
  id: string;
  sku: string;
  name: string;
  latin: string;
  price: number;
  image: string;
  size: string;
  stock?: number;
  catalogSection: string;
  plantKind?: string;
  lightLevel?: string;
  heightClass?: string;
  petSafety?: string;
  careLevel?: string;
  placement?: string;
  watering?: string;
  categoryId?: number;
  rating: number;
  reviewsCount: number;
  popularityScore?: number;
  collections?: string[];
  filterAttributes?: FilterAttribute[];
};

type Category = { id: number; parentId: number | null; name: string; slug: string; sortOrder: number; icon: string };
type CategoryNode = { id: number; name: string; slug: string; icon: string; count: number; children: CategoryNode[] };
type Landing = { type: "category" | "collection"; slug: string };
type CollectionMeta = { slug: string; title: string; note: string; count?: number };
type RangeValue = { min: string; max: string };
type Facet = {
  name: string;
  unit?: string;
  values: Set<string>;
  displayMode: FilterDisplayMode;
  dataType?: FilterDataType;
  numericValues: number[];
};

const money = (value: number) => new Intl.NumberFormat("ru-RU", {
  style: "currency",
  currency: "RUB",
  maximumFractionDigits: 0,
}).format(value);

const categoryIconPaths: Record<string, string> = {
  leaf: "M19 4C11 4 5 8 5 15c5 1 10-2 14-11ZM5 20c2-6 6-9 11-12",
  pot: "M6 9h12l-1 10H7L6 9Zm-1-3h14v3H5V6Z",
  soil: "M4 8c4-3 12-3 16 0v10H4V8Zm2 3c4-2 8-2 12 0",
  fertilizer: "M9 3h6v4l3 4v9H6v-9l3-4V3Zm0 10h6",
  tools: "M14 5a4 4 0 0 0 5 5l-9 9-4-4 9-9",
};

function initialAttributeFilters() {
  const result: Record<string, string> = {};
  new URLSearchParams(window.location.search).forEach((value, key) => {
    if (key.startsWith("filter.")) result[key.slice(7)] = value;
  });
  return result;
}

function initialRangeFilters() {
  const result: Record<string, RangeValue> = {};
  new URLSearchParams(window.location.search).forEach((value, key) => {
    if (key.startsWith("min.")) {
      const code = key.slice(4);
      result[code] = { min: value, max: result[code]?.max || "" };
    }
    if (key.startsWith("max.")) {
      const code = key.slice(4);
      result[code] = { min: result[code]?.min || "", max: value };
    }
  });
  return result;
}

function CatalogDropdown({ label, value, options, onChange, className = "" }: { label: string; value: string; options: Array<{ value: string; label: string }>; onChange: (value: string) => void; className?: string }) {
  const selected = options.find((option) => option.value === value)?.label;
  return <details className={`catalog-dropdown ${className}`}>
    <summary><span>{selected || label}</span><i>⌄</i></summary>
    <div role="listbox" aria-label={label}>
      <button type="button" className={!value ? "active" : ""} onClick={(event) => { onChange(""); event.currentTarget.closest("details")?.removeAttribute("open"); }}>Все варианты</button>
      {options.map((option) => <button type="button" role="option" aria-selected={value === option.value} className={value === option.value ? "active" : ""} key={option.value} onClick={(event) => { onChange(option.value); event.currentTarget.closest("details")?.removeAttribute("open"); }}>{option.label}<span>{value === option.value ? "✓" : ""}</span></button>)}
    </div>
  </details>;
}

function CategoryIcon({ name }: { name: string }) {
  return <svg className="category-icon" viewBox="0 0 24 24" aria-hidden="true"><path d={categoryIconPaths[name] || categoryIconPaths.leaf} /></svg>;
}

function attributeValues(attribute: FilterAttribute): Array<string | number | boolean> {
  return Array.isArray(attribute.value) ? attribute.value : [attribute.value];
}

function inferredDisplayMode(attribute: FilterAttribute): FilterDisplayMode {
  if (attribute.displayMode) return attribute.displayMode;
  if (attribute.dataType === "number" || attributeValues(attribute).some((value) => typeof value === "number")) return "range";
  if (attribute.dataType === "boolean" || attributeValues(attribute).some((value) => typeof value === "boolean")) return "chips";
  return "select";
}

export default function StorefrontPage({ landing }: { landing?: Landing }) {
  const [products, setProducts] = useState<Product[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [categoriesLoaded, setCategoriesLoaded] = useState(false);
  const [collectionMeta, setCollectionMeta] = useState<CollectionMeta | null>(null);
  const [collectionState, setCollectionState] = useState<"idle" | "loading" | "loaded" | "error">(landing?.type === "collection" ? "loading" : "idle");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState(() => new URLSearchParams(window.location.search).get("q") ?? "");
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [opened, setOpened] = useState<Set<number>>(new Set());
  const [inStockOnly, setInStockOnly] = useState(() => new URLSearchParams(window.location.search).get("stock") === "1");
  const [attributeFilters, setAttributeFilters] = useState<Record<string, string>>(initialAttributeFilters);
  const [rangeFilters, setRangeFilters] = useState<Record<string, RangeValue>>(initialRangeFilters);
  const [sort, setSort] = useState(() => new URLSearchParams(window.location.search).get("sort") || "popular");
  const [viewMode, setViewMode] = useState<"grid" | "list">("grid");
  const [visibleLimit, setVisibleLimit] = useState(12);

  useEffect(() => {
    const closeDropdowns = (event: PointerEvent) => document.querySelectorAll<HTMLDetailsElement>(".catalog-dropdown[open]").forEach((details) => {
      if (!details.contains(event.target as Node)) details.removeAttribute("open");
    });
    document.addEventListener("pointerdown", closeDropdowns);
    return () => document.removeEventListener("pointerdown", closeDropdowns);
  }, []);

  const [cart, setCart] = useSharedCart();
  const [favorites, setFavorites] = useState<Set<string>>(() => {
    try {
      return new Set(JSON.parse(window.localStorage.getItem("ficusin-favorites") || "[]") as string[]);
    } catch {
      return new Set();
    }
  });
  const [cartOpen, setCartOpen] = useState(false);

  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.has("cart")) window.location.replace("/cart");
  }, []);

  useEffect(() => {
    fetch("/api/v1/catalog")
      .then((response) => response.json())
      .then((data: { products?: Product[] }) => {
        setProducts(data.products ?? []);
        setError(data.products?.length ? "" : "Каталог пока пуст");
      })
      .catch(() => setError("Каталог временно недоступен"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    fetch("/api/v1/categories")
      .then((response) => response.json())
      .then((data: { categories?: Category[] }) => setCategories(data.categories ?? []))
      .catch(() => setCategories([]))
      .finally(() => setCategoriesLoaded(true));
  }, []);

  useEffect(() => {
    if (landing?.type !== "collection") return;
    setCollectionState("loading");
    fetch("/api/v1/collections", { cache: "no-store" })
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("collections unavailable")))
      .then((body: { collections?: CollectionMeta[] }) => {
        setCollectionMeta(body.collections?.find((item) => item.slug === landing.slug) ?? null);
        setCollectionState("loaded");
      })
      .catch(() => {
        setCollectionMeta(null);
        setCollectionState("error");
      });
  }, [landing?.type, landing?.slug]);

  const activeCategory = useMemo(
    () => landing?.type === "category" ? categories.find((item) => item.slug === landing.slug) ?? null : null,
    [categories, landing],
  );
  const category = activeCategory?.id ?? null;
  const categoryName = activeCategory?.name ?? "";
  const searching = query.trim().length > 0;
  const categoryNotFound = landing?.type === "category" && categoriesLoaded && !activeCategory;
  const collectionNotFound = landing?.type === "collection" && collectionState === "loaded" && !collectionMeta;

  useEffect(() => {
    if (!activeCategory) return;
    const parents = new Map(categories.map((item) => [item.id, item.parentId]));
    const path = new Set<number>();
    let current: number | null = activeCategory.id;
    while (current != null) {
      path.add(current);
      current = parents.get(current) ?? null;
    }
    setOpened(path);
  }, [activeCategory, categories]);

  useEffect(() => {
    const url = new URL(window.location.href);
    const managed = [...url.searchParams.keys()].filter((key) => key === "q" || key === "stock" || key === "sort" || key.startsWith("filter.") || key.startsWith("min.") || key.startsWith("max."));
    managed.forEach((key) => url.searchParams.delete(key));
    if (query.trim()) url.searchParams.set("q", query.trim());
    if (inStockOnly) url.searchParams.set("stock", "1");
    if (sort !== "popular") url.searchParams.set("sort", sort);
    Object.entries(attributeFilters).forEach(([code, value]) => { if (value) url.searchParams.set(`filter.${code}`, value); });
    Object.entries(rangeFilters).forEach(([code, value]) => {
      if (value.min) url.searchParams.set(`min.${code}`, value.min);
      if (value.max) url.searchParams.set(`max.${code}`, value.max);
    });
    window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  }, [query, inStockOnly, sort, attributeFilters, rangeFilters]);

  const tree = useMemo(() => {
    const kids = new Map<number | null, Category[]>();
    categories.forEach((item) => {
      const key = item.parentId ?? null;
      kids.set(key, [...(kids.get(key) ?? []), item]);
    });
    const direct = new Map<number, number>();
    products.forEach((item) => {
      if (item.categoryId == null) return;
      direct.set(item.categoryId, (direct.get(item.categoryId) ?? 0) + 1);
    });
    const order = (list: Category[]) => [...list].sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name));
    const build = (item: Category): CategoryNode => {
      let children = order(kids.get(item.id) ?? []);
      while (children.length === 1 && (direct.get(children[0].id) ?? 0) === 0) children = order(kids.get(children[0].id) ?? []);
      const nodes = children.map(build);
      return {
        id: item.id,
        name: item.name,
        slug: item.slug,
        icon: item.icon || "leaf",
        count: (direct.get(item.id) ?? 0) + nodes.reduce((sum, node) => sum + node.count, 0),
        children: nodes,
      };
    };
    return order(kids.get(null) ?? []).map(build);
  }, [categories, products]);

  const headerMenus = useMemo(() => {
    const children = new Map<number | null, Category[]>();
    categories.forEach((item) => children.set(item.parentId, [...(children.get(item.parentId) || []), item]));
    const order = (items: Category[]) => [...items].sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name, "ru"));
    const roots: HeaderMenuItem[] = order(children.get(null) || []).map((item) => ({ id: item.id, label: item.name, slug: item.slug }));
    const plantRoot = categories.find((item) => item.parentId == null && /растен/i.test(item.name));
    if (!plantRoot) return { catalog: roots, plants: [] };
    const collectPlantKinds = (parentId: number): Category[] => order(children.get(parentId) || []).flatMap((item): Category[] => {
      const nested = children.get(item.id) || [];
      return nested.length ? collectPlantKinds(item.id) : [item];
    });
    return { catalog: roots, plants: collectPlantKinds(plantRoot.id).map((item) => ({ id: item.id, label: item.name, slug: item.slug })) };
  }, [categories]);

  const inBranch = useMemo(() => {
    const parents = new Map(categories.map((item) => [item.id, item.parentId]));
    return (productCategory: number | undefined, branch: number): boolean => {
      let current = productCategory ?? null;
      while (current != null) {
        if (current === branch) return true;
        current = parents.get(current) ?? null;
      }
      return false;
    };
  }, [categories]);

  const contextProducts = useMemo(() => {
    if (landing?.type === "collection") return products.filter((product) => product.collections?.includes(landing.slug));
    if (category != null) return products.filter((product) => inBranch(product.categoryId, category));
    return products;
  }, [products, landing, category, inBranch]);

  // Existing documented behaviour is preserved: a search started from a
  // category or collection searches the whole catalogue. The UI below makes
  // that scope explicit instead of silently changing context.
  const found = useMemo(() => searching ? searchProducts(products, query) : contextProducts, [products, contextProducts, query, searching]);

  const facetPopulation = useMemo(() => {
    if (searching) return [];
    if (landing?.type === "collection") return contextProducts;
    if (category != null) return contextProducts;
    return [];
  }, [searching, landing, category, contextProducts]);

  const facets = useMemo(() => {
    const result = new Map<string, Facet>();
    facetPopulation.forEach((product) => product.filterAttributes?.filter((attribute) => attribute.filterable).forEach((attribute) => {
      const mode = inferredDisplayMode(attribute);
      const facet = result.get(attribute.code) || {
        name: attribute.name,
        unit: attribute.unit,
        values: new Set<string>(),
        displayMode: mode,
        dataType: attribute.dataType,
        numericValues: [],
      };
      attributeValues(attribute).forEach((value) => {
        if (value === null || value === undefined || String(value) === "") return;
        facet.values.add(String(value));
        if (typeof value === "number" || (mode === "range" && Number.isFinite(Number(value)))) facet.numericValues.push(Number(value));
      });
      result.set(attribute.code, facet);
    }));
    return [...result.entries()].filter(([, facet]) => facet.values.size > 0);
  }, [facetPopulation]);

  const facetCodes = useMemo(() => new Set(facets.map(([code]) => code)), [facets]);
  useEffect(() => {
    setAttributeFilters((current) => Object.fromEntries(Object.entries(current).filter(([code]) => facetCodes.has(code))));
    setRangeFilters((current) => Object.fromEntries(Object.entries(current).filter(([code]) => facetCodes.has(code))));
  }, [facetCodes]);

  const activeFilterCount = useMemo(() => {
    const attributes = Object.values(attributeFilters).filter(Boolean).length;
    const ranges = Object.values(rangeFilters).filter((value) => value.min || value.max).length;
    return attributes + ranges + (inStockOnly ? 1 : 0);
  }, [attributeFilters, rangeFilters, inStockOnly]);

  const visible = useMemo(() => {
    let list = found;
    if (inStockOnly) list = list.filter((item) => (item.stock ?? 0) > 0);
    for (const [code, selected] of Object.entries(attributeFilters)) {
      if (!selected) continue;
      list = list.filter((product) => product.filterAttributes?.some((attribute) => attribute.code === code && attributeValues(attribute).some((value) => String(value) === selected)));
    }
    for (const [code, range] of Object.entries(rangeFilters)) {
      if (!range.min && !range.max) continue;
      const minimum = range.min === "" ? -Infinity : Number(range.min);
      const maximum = range.max === "" ? Infinity : Number(range.max);
      list = list.filter((product) => product.filterAttributes?.some((attribute) => attribute.code === code && attributeValues(attribute).some((value) => {
        const numeric = Number(value);
        return Number.isFinite(numeric) && numeric >= minimum && numeric <= maximum;
      })));
    }
    if (sort === "cheap") list = [...list].sort((a, b) => a.price - b.price);
    if (sort === "expensive") list = [...list].sort((a, b) => b.price - a.price);
    if (sort === "popular") list = [...list].sort((a, b) => (b.popularityScore ?? 0) - (a.popularityScore ?? 0));
    return list;
  }, [found, inStockOnly, attributeFilters, rangeFilters, sort]);

  const landingTitle = landing?.type === "collection" ? collectionMeta?.title || "Подборка растений" : categoryName || "Каталог";
  const landingDescription = landing?.type === "collection"
    ? collectionMeta?.note || `Товары из подборки «${landingTitle}».`
    : `${landingTitle}: актуальные цены, наличие и доставка по России.`;

  useEffect(() => {
    if (loading) return;
    track("view_item_list", { properties: { list: landing ? landingTitle : categoryName || "catalog", items: visible.length } });
  }, [loading, categoryName, landing, landingTitle, visible.length]);

  useEffect(() => {
    if (!query.trim()) return;
    const timer = window.setTimeout(() => track("search", { properties: { query: query.trim().slice(0, 120), results: visible.length } }), 700);
    return () => window.clearTimeout(timer);
  }, [query, visible.length]);

  useEffect(() => {
    if (!category && !landing && !inStockOnly && !Object.values(attributeFilters).some(Boolean) && !Object.values(rangeFilters).some((value) => value.min || value.max)) return;
    const timer = window.setTimeout(() => track("filter", { properties: { category, collection: landing?.type === "collection" ? landing.slug : undefined, inStockOnly, attributes: attributeFilters, ranges: rangeFilters, results: visible.length } }), 500);
    return () => window.clearTimeout(timer);
  }, [category, landing, inStockOnly, attributeFilters, rangeFilters, visible.length]);

  const clearAdditionalFilters = () => {
    setInStockOnly(false);
    setAttributeFilters({});
    setRangeFilters({});
  };

  const toggleCategory = (id: number) => setOpened((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });

  const branch = (node: CategoryNode, depth: number) => (
    <div key={node.id}>
      <a href={`/catalog/${encodeURIComponent(node.slug)}`} className={category === node.id ? "active" : ""} style={{ paddingLeft: 10 + depth * 14 }} aria-current={category === node.id ? "page" : undefined}>
        <span>{node.children.length > 0 && <i className={opened.has(node.id) ? "twist open" : "twist"} aria-hidden="true" onClick={(event) => { event.preventDefault(); toggleCategory(node.id); }}>›</i>}<CategoryIcon name={node.icon} />{node.name}</span>
        <small>{node.count}</small>
      </a>
      {opened.has(node.id) && node.children.map((child) => branch(child, depth + 1))}
    </div>
  );

  const renderFacet = ([code, facet]: [string, Facet]) => {
    if (facet.displayMode === "range") {
      const values = facet.numericValues.filter(Number.isFinite);
      const minimum = values.length ? Math.min(...values) : undefined;
      const maximum = values.length ? Math.max(...values) : undefined;
      const selected = rangeFilters[code] || { min: "", max: "" };
      return <fieldset key={code} className="catalog-range-filter"><legend>{facet.name}{facet.unit ? `, ${facet.unit}` : ""}</legend><label>От<input aria-label={`${facet.name}: от`} type="number" min={minimum} max={maximum} value={selected.min} onChange={(event) => setRangeFilters((current) => ({ ...current, [code]: { min: event.target.value, max: current[code]?.max || "" } }))} /></label><label>До<input aria-label={`${facet.name}: до`} type="number" min={minimum} max={maximum} value={selected.max} onChange={(event) => setRangeFilters((current) => ({ ...current, [code]: { min: current[code]?.min || "", max: event.target.value } }))} /></label></fieldset>;
    }
    const values = [...facet.values].sort((a, b) => a.localeCompare(b, "ru", { numeric: true }));
    const isBoolean = facet.dataType === "boolean" || values.every((value) => value === "true" || value === "false");
    if (facet.displayMode === "chips" || isBoolean) {
      return <fieldset key={code} className="catalog-chip-filter"><legend>{facet.name}</legend><div>{values.map((value) => <button type="button" key={value} className={attributeFilters[code] === value ? "active" : ""} aria-pressed={attributeFilters[code] === value} onClick={() => setAttributeFilters((current) => ({ ...current, [code]: current[code] === value ? "" : value }))}>{isBoolean ? (value === "true" ? "Да" : "Нет") : attributeLabel(value)}</button>)}</div></fieldset>;
    }
    return <label key={code}>{facet.name}{facet.unit ? `, ${facet.unit}` : ""}<select value={attributeFilters[code] || ""} onChange={(event) => setAttributeFilters((current) => ({ ...current, [code]: event.target.value }))}><option value="">Любое значение</option>{values.map((value) => <option key={value} value={value}>{attributeLabel(value)}</option>)}</select></label>;
  };

  const cartCount = Object.values(cart).reduce((sum, value) => sum + value, 0);
  const addToCart = (product: Product) => {
    track("add_to_cart", { productCode: product.id, sku: product.sku, value: product.price, quantity: 1, properties: { name: product.name, category: product.catalogSection, list: landing ? landingTitle : categoryName || "catalog" } });
    setCart((current) => ({ ...current, [product.sku]: Math.min(product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20, (current[product.sku] ?? 0) + 1) }));
  };
  const changeCartQuantity = (product: Product, delta: number) => {
    track(delta > 0 ? "add_to_cart" : "remove_from_cart", { productCode: product.id, sku: product.sku, value: product.price, quantity: 1, properties: { name: product.name, category: product.catalogSection, list: landing ? landingTitle : categoryName || "catalog" } });
    setCart((current) => {
      const maximum = product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20;
      const nextQuantity = Math.max(0, Math.min(maximum, (current[product.sku] || 0) + delta));
      if (nextQuantity === 0) { const next = { ...current }; delete next[product.sku]; return next; }
      return { ...current, [product.sku]: nextQuantity };
    });
  };
  const toggleFavorite = (id: string) => setFavorites((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id); else next.add(id);
    window.localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
    return next;
  });

  if (categoryNotFound || collectionNotFound) return <NotFoundPage />;

  return (
    <main className="storefront">
      <StoreHeader cartCount={cartCount} favoritesCount={favorites.size} query={query} onQueryChange={setQuery} onCartClick={() => window.location.assign("/cart")} homeNavigation catalogMenuItems={headerMenus.catalog} plantMenuItems={headerMenus.plants} />

      {landing ? <section className="catalog-landing-hero"><nav aria-label="Хлебные крошки"><a href="/">Главная</a><span>/</span><a href="/#catalog">Каталог</a></nav><h1>{landingTitle}</h1><p>{landingDescription}</p></section> : <section className="home-hero" aria-labelledby="home-title">
        <div className="home-hero-copy"><h1 id="home-title">Растения,<br />с которыми<br /><em>хорошо</em><i>.</i></h1><p>Живые растения для дома и офиса.<br />Выбираем лучшее и доставляем по всей России.</p><div className="home-hero-actions"><a href="#catalog">Выбрать своё растение <span>→</span></a><button type="button" aria-label="Видео о Фикусин"><b>▶</b> Видео о Фикусин</button></div><div className="home-team"><img src="/assets/redesign/team-avatars.webp" alt="Команда Фикусин" /><span>За вашими растениями<br />ухаживает <b>команда любителей</b></span></div></div>
        <div className="home-hero-visual"><img src="/assets/redesign/home-hero-4k.webp" alt="Алоказия в керамическом кашпо" /><span className="home-stamp" aria-label="Живые растения для живых людей"><svg viewBox="0 0 132 132" aria-hidden="true"><defs><path id="stamp-top" d="M25 64 A41 41 0 0 1 107 64" /><path id="stamp-bottom" d="M25 74 A41 41 0 0 0 107 74" /></defs><circle cx="66" cy="66" r="58" /><circle cx="66" cy="66" r="52" strokeDasharray="2 4" /><text><textPath href="#stamp-top" startOffset="50%" textAnchor="middle">ЖИВЫЕ РАСТЕНИЯ</textPath></text><text><textPath href="#stamp-bottom" startOffset="50%" textAnchor="middle">ДЛЯ ЖИВЫХ ЛЮДЕЙ</textPath></text><path className="stamp-plant" d="M66 82V55m0 10c-12 0-16-8-16-14 9 0 16 4 16 14Zm0 7c12 0 16-8 16-14-9 0-16 4-16 14ZM55 85h22" /></svg></span><span className="home-note delivery"><svg className="note-icon" viewBox="0 0 48 58" aria-hidden="true"><path d="M9 24 24 16l15 8v25L24 56 9 49V24Zm15-8v40m-15-32 15 8 15-8M24 16V3m0 9c-8 0-11-5-11-10 7 0 11 3 11 10Zm0-2c7 0 10-5 10-9-6 0-10 3-10 9Z" /></svg><b>Доставка<br />по всей России</b></span><span className="home-note packing"><svg className="note-icon" viewBox="0 0 48 58" aria-hidden="true"><path d="M9 24 24 16l15 8v25L24 56 9 49V24Zm15-8v40m-15-32 15 8 15-8M24 16V3m0 9c-8 0-11-5-11-10 7 0 11 3 11 10Zm0-2c7 0 10-5 10-9-6 0-10 3-10 9Z" /></svg><b>Аккуратно упакуем<br />и довезём в лучшем виде</b><i>→</i></span></div>
      </section>}

      <CollectionStrip products={products} activeSlug={landing?.type === "collection" ? landing.slug : undefined} />

      <section className="storefront-shell" id="catalog">
        <aside className="storefront-side">
          <nav className="storefront-tree">
            <a href="/#catalog" className={!landing && category == null ? "active" : ""}><span>Весь каталог</span><small>{products.length}</small></a>
            {tree.map((root) => branch(root, 0))}
          </nav>
          <details className="storefront-filters" open={filtersOpen} onToggle={(event) => setFiltersOpen(event.currentTarget.open)}>
            <summary>Подбор по характеристикам{activeFilterCount > 0 && <b>{activeFilterCount}</b>}</summary>
            <label className="storefront-check"><input type="checkbox" checked={inStockOnly} onChange={(event) => setInStockOnly(event.target.checked)} />Только в наличии</label>
            {facets.length > 0 && <div className="storefront-attribute-filters"><p className="storefront-side-title">Характеристики</p>{facets.map(renderFacet)}</div>}
          </details>
        </aside>

        <div className="storefront-main">
          <div className="storefront-head"><div><h2>{searching ? "Результаты поиска" : landing ? landingTitle : "Каталог"}</h2><p>{searching ? `Нашли ${visible.length}` : `${visible.length} товаров`}{searching && <span> по запросу «{query.trim()}»</span>}</p>{searching && landing && <p className="catalog-search-scope">Поиск выполняется по всему каталогу, без ограничения текущей {landing.type === "category" ? "категорией" : "подборкой"}.</p>}</div></div>

          <div className="home-catalog-toolbar">
            <button type="button" className={filtersOpen ? "home-filter-button active" : "home-filter-button"} aria-expanded={filtersOpen} onClick={() => setFiltersOpen((value) => !value)}><span className="filter-sliders" aria-hidden="true">☷</span><span>Фильтры</span><i>⌄</i>{activeFilterCount > 0 && <b>{activeFilterCount}</b>}</button>
            <div className="home-filter-group">{facets.slice(0, 6).filter(([, facet]) => facet.displayMode === "select" && facet.dataType !== "boolean").map(([code, facet]) => <CatalogDropdown key={code} label={facet.name} value={attributeFilters[code] || ""} onChange={(value) => setAttributeFilters((current) => ({ ...current, [code]: value }))} options={[...facet.values].sort((a, b) => a.localeCompare(b, "ru", { numeric: true })).map((value) => ({ value, label: `${attributeLabel(value)}${facet.unit ? ` ${facet.unit}` : ""}` }))} />)}</div>
            <CatalogDropdown className="home-sort" label="По популярности" value={sort} onChange={setSort} options={[{ value: "popular", label: "По популярности" }, { value: "cheap", label: "Сначала дешевле" }, { value: "expensive", label: "Сначала дороже" }]} />
            <div className="catalog-view-toggle" aria-label="Вид каталога"><button type="button" className={viewMode === "grid" ? "active" : ""} onClick={() => setViewMode("grid")} aria-label="Плитка">⊞</button><button type="button" className={viewMode === "list" ? "active" : ""} onClick={() => setViewMode("list")} aria-label="Список">☰</button></div>
          </div>
          {filtersOpen && <div className="home-filter-panel"><strong>Все фильтры</strong><label className="storefront-check"><input type="checkbox" checked={inStockOnly} onChange={(event) => setInStockOnly(event.target.checked)} />Только в наличии</label><button type="button" onClick={clearAdditionalFilters}>Сбросить фильтры</button></div>}

          {loading && <p className="storefront-empty">Загружаем каталог…</p>}
          {!loading && error && <p className="storefront-empty">{error}</p>}
          {!loading && !error && visible.length === 0 && <div className="storefront-empty"><strong>{searching ? "Ничего не нашли" : "Здесь пока пусто"}</strong><p>{searching ? "Проверьте написание или поищите короче — например, «фикус» вместо «фикус бенджамина большой»." : activeFilterCount > 0 ? "По текущим фильтрам товаров нет. Сбросьте дополнительные фильтры или измените условия." : landing?.type === "collection" ? "В этой подборке сейчас нет доступных товаров." : "В этой категории сейчас нет доступных товаров."}</p>{activeFilterCount > 0 ? <button type="button" onClick={clearAdditionalFilters}>Сбросить фильтры</button> : <a href="/#catalog">Показать весь каталог</a>}</div>}

          <div className={`storefront-grid ${viewMode === "list" ? "list-view" : ""}`}>
            {visible.slice(0, visibleLimit).map((product) => {
              const inCart = cart[product.sku] ?? 0;
              const preorder = (product.stock ?? 0) <= 0;
              return <article key={product.id} className={preorder ? "storefront-card preorder" : "storefront-card"}>
                <button className={favorites.has(product.id) ? "storefront-fav active" : "storefront-fav"} onClick={() => toggleFavorite(product.id)} aria-label="В избранное">♥</button>
                <a className="storefront-image" href={`/product/${product.id}`} onClick={() => track("select_item", { productCode: product.id, sku: product.sku, value: product.price, properties: { list: landing ? landingTitle : categoryName || "catalog" } })}><img src={product.image} alt={product.name} loading="lazy" /></a>
                <a className="storefront-name" href={`/product/${product.id}`} onClick={() => track("select_item", { productCode: product.id, sku: product.sku, value: product.price, properties: { list: landing ? landingTitle : categoryName || "catalog" } })}>{product.name}</a>
                {product.filterAttributes?.some((attribute) => attribute.badge) && <div className="storefront-attribute-badges">{product.filterAttributes.filter((attribute) => attribute.badge).slice(0, 2).map((attribute) => <span key={attribute.code}>{attribute.name}: {attributeValue(attribute.value, attribute.unit)}</span>)}</div>}
                {product.latin && <p className="storefront-latin">{product.latin}</p>}
                {product.reviewsCount > 0 && <p className="storefront-rating"><span>★</span> {product.rating.toFixed(1)} <small>({product.reviewsCount})</small></p>}
                <div className="storefront-buy"><span className="storefront-price"><strong>{money(product.price)}</strong>{preorder && <em>Под заказ</em>}</span>{inCart > 0 ? <div className="storefront-quantity"><button type="button" onClick={() => changeCartQuantity(product, -1)} aria-label="Уменьшить количество">−</button><button type="button" className="quantity-value" onClick={() => window.location.assign("/cart")} aria-label={`В корзине · ${inCart}`}>{inCart}</button><button type="button" onClick={() => changeCartQuantity(product, 1)} aria-label="Увеличить количество">+</button></div> : <button onClick={() => addToCart(product)} aria-label="В корзину" title="Добавить в корзину"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4.5 7.5h2l1.4 9.2h9.8l1.8-6.5H7.1M9.5 20a1 1 0 1 0 0-2 1 1 0 0 0 0 2Zm7 0a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z" /></svg><span className="cart-button-label">В корзину</span></button>}</div>
              </article>;
            })}
          </div>
          {visible.length > visibleLimit && <button className="storefront-more" type="button" onClick={() => setVisibleLimit((value) => value + 12)}>Показать ещё растения <span>⌄</span></button>}
        </div>
      </section>
      <CheckoutHost cart={cart} products={products} cartOpen={cartOpen} onCartOpenChange={setCartOpen} onCartChange={setCart} />
    </main>
  );
}
