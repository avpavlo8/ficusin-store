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
  value: string | number | boolean | string[];
  filterable: boolean;
  badge: boolean;
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
  const [visibleLimit, setVisibleLimit] = useState(12);

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
  const routePending = landing?.type === "category"
    ? !categoriesLoaded
    : landing?.type === "collection"
      ? collectionState === "loading"
      : false;

  const activeCategoryPath = useMemo(() => {
    const path = new Set<number>();
    if (!activeCategory) return path;
    const parents = new Map(categories.map((item) => [item.id, item.parentId]));
    let current: number | null = activeCategory.id;
    while (current != null) {
      path.add(current);
      current = parents.get(current) ?? null;
    }
    return path;
  }, [activeCategory, categories]);

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
    if (landing?.type === "category" && !activeCategory) return [];
    if (category != null) return products.filter((product) => inBranch(product.categoryId, category));
    return products;
  }, [products, landing, activeCategory, category, inBranch]);

  // This is the existing documented search rule: search is global even when
  // started from a category or collection. We keep it and explain the scope in
  // the UI rather than silently inventing a different product rule.
  const found = useMemo(() => searching ? searchProducts(products, query) : contextProducts, [products, contextProducts, query, searching]);

  // Facets belong to the current catalogue context, not to the global search
  // result. A global catalogue has no selected category/collection, so it has
  // no attribute facets. Category and collection pages keep their own context
  // even though search results themselves remain global by documented rule.
  const facetPopulation = useMemo(() => {
    if (landing?.type === "collection") return contextProducts;
    if (category != null) return contextProducts;
    return [];
  }, [landing, category, contextProducts]);

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
  const compatibleAttributeFilters = useMemo(() => {
    if (routePending || loading) return attributeFilters;
    return Object.fromEntries(Object.entries(attributeFilters).filter(([code]) => facetCodes.has(code)));
  }, [attributeFilters, facetCodes, routePending, loading]);
  const compatibleRangeFilters = useMemo(() => {
    if (routePending || loading) return rangeFilters;
    return Object.fromEntries(Object.entries(rangeFilters).filter(([code]) => facetCodes.has(code)));
  }, [rangeFilters, facetCodes, routePending, loading]);

  useEffect(() => {
    const url = new URL(window.location.href);
    const managed: string[] = [];
    url.searchParams.forEach((_value, key) => {
      if (key === "q" || key === "stock" || key === "sort" || key.startsWith("filter.") || key.startsWith("min.") || key.startsWith("max.")) managed.push(key);
    });
    managed.forEach((key) => url.searchParams.delete(key));
    if (query.trim()) url.searchParams.set("q", query.trim());
    if (inStockOnly) url.searchParams.set("stock", "1");
    if (sort !== "popular") url.searchParams.set("sort", sort);
    Object.entries(compatibleAttributeFilters).forEach(([code, value]) => { if (value) url.searchParams.set(`filter.${code}`, value); });
    Object.entries(compatibleRangeFilters).forEach(([code, value]) => {
      if (value.min) url.searchParams.set(`min.${code}`, value.min);
      if (value.max) url.searchParams.set(`max.${code}`, value.max);
    });
    window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  }, [query, inStockOnly, sort, compatibleAttributeFilters, compatibleRangeFilters]);

  const activeFilterCount = useMemo(() => {
    const attributes = Object.values(compatibleAttributeFilters).filter(Boolean).length;
    const ranges = Object.values(compatibleRangeFilters).filter((value) => value.min || value.max).length;
    return attributes + ranges + (inStockOnly ? 1 : 0);
  }, [compatibleAttributeFilters, compatibleRangeFilters, inStockOnly]);

  const activeFilterChips = useMemo(() => {
    const chips: Array<{ key: string; label: string; remove: () => void }> = [];
    if (inStockOnly) chips.push({ key: "stock", label: "Только в наличии", remove: () => setInStockOnly(false) });
    Object.entries(compatibleAttributeFilters).forEach(([code, value]) => {
      if (!value) return;
      const facet = facets.find(([facetCode]) => facetCode === code)?.[1];
      chips.push({ key: `attribute-${code}`, label: `${facet?.name || code}: ${attributeLabel(value)}`, remove: () => setAttributeFilters((current) => ({ ...current, [code]: "" })) });
    });
    Object.entries(compatibleRangeFilters).forEach(([code, range]) => {
      if (!range.min && !range.max) return;
      const facet = facets.find(([facetCode]) => facetCode === code)?.[1];
      const bounds = range.min && range.max ? `${range.min}–${range.max}` : range.min ? `от ${range.min}` : `до ${range.max}`;
      chips.push({ key: `range-${code}`, label: `${facet?.name || code}: ${bounds}${facet?.unit ? ` ${facet.unit}` : ""}`, remove: () => setRangeFilters((current) => ({ ...current, [code]: { min: "", max: "" } })) });
    });
    return chips;
  }, [compatibleAttributeFilters, compatibleRangeFilters, facets, inStockOnly]);

  const visible = useMemo(() => {
    let list = found;
    if (inStockOnly) list = list.filter((item) => (item.stock ?? 0) > 0);
    for (const [code, selected] of Object.entries(compatibleAttributeFilters)) {
      if (!selected) continue;
      list = list.filter((product) => product.filterAttributes?.some((attribute) => attribute.code === code && attributeValues(attribute).some((value) => String(value) === selected)));
    }
    for (const [code, range] of Object.entries(compatibleRangeFilters)) {
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
  }, [found, inStockOnly, compatibleAttributeFilters, compatibleRangeFilters, sort]);

  const landingTitle = landing?.type === "collection" ? collectionMeta?.title || "Подборка растений" : categoryName || "Каталог";
  const landingDescription = landing?.type === "collection"
    ? collectionMeta?.note || `Товары из подборки «${landingTitle}».`
    : `${landingTitle}: актуальные цены, наличие и доставка по России.`;

  useEffect(() => {
    if (loading || routePending) return;
    track("view_item_list", { properties: { list: landing ? landingTitle : categoryName || "catalog", items: visible.length } });
  }, [loading, routePending, categoryName, landing, landingTitle, visible.length]);

  useEffect(() => {
    if (!query.trim()) return;
    const timer = window.setTimeout(() => track("search", { properties: { query: query.trim().slice(0, 120), results: visible.length } }), 700);
    return () => window.clearTimeout(timer);
  }, [query, visible.length]);

  useEffect(() => {
    if (!category && !landing && !inStockOnly && !Object.values(compatibleAttributeFilters).some(Boolean) && !Object.values(compatibleRangeFilters).some((value) => value.min || value.max)) return;
    const timer = window.setTimeout(() => track("filter", { properties: { category, collection: landing?.type === "collection" ? landing.slug : undefined, inStockOnly, attributes: compatibleAttributeFilters, ranges: compatibleRangeFilters, results: visible.length } }), 500);
    return () => window.clearTimeout(timer);
  }, [category, landing, inStockOnly, compatibleAttributeFilters, compatibleRangeFilters, visible.length]);

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

  const branch = (node: CategoryNode, depth: number) => {
    const expanded = opened.has(node.id) || activeCategoryPath.has(node.id);
    return <div key={node.id}>
      <a href={`/catalog/${encodeURIComponent(node.slug)}`} className={category === node.id ? "active" : ""} style={{ paddingLeft: 10 + depth * 14 }} aria-current={category === node.id ? "page" : undefined}>
        <span>{node.children.length > 0 && <i className={expanded ? "twist open" : "twist"} aria-hidden="true" onClick={(event) => { event.preventDefault(); toggleCategory(node.id); }}>›</i>}<CategoryIcon name={node.icon} />{node.name}</span>
        <small>{node.count}</small>
      </a>
      {expanded && node.children.map((child) => branch(child, depth + 1))}
    </div>;
  };

  const renderFacet = ([code, facet]: [string, Facet]) => {
    if (facet.displayMode === "range") {
      const values = facet.numericValues.filter(Number.isFinite);
      const minimum = values.length ? Math.min(...values) : undefined;
      const maximum = values.length ? Math.max(...values) : undefined;
      const selected = compatibleRangeFilters[code] || { min: "", max: "" };
      return <fieldset key={code} className="catalog-range-filter"><legend>{facet.name}{facet.unit ? `, ${facet.unit}` : ""}</legend><label>От<input aria-label={`${facet.name}: от`} type="number" min={minimum} max={maximum} value={selected.min} onChange={(event) => setRangeFilters((current) => ({ ...current, [code]: { min: event.target.value, max: current[code]?.max || "" } }))} /></label><label>До<input aria-label={`${facet.name}: до`} type="number" min={minimum} max={maximum} value={selected.max} onChange={(event) => setRangeFilters((current) => ({ ...current, [code]: { min: current[code]?.min || "", max: event.target.value } }))} /></label></fieldset>;
    }
    const values = [...facet.values].sort((a, b) => a.localeCompare(b, "ru", { numeric: true }));
    const isBoolean = facet.dataType === "boolean" || values.every((value) => value === "true" || value === "false");
    if (facet.displayMode === "chips") {
      return <fieldset key={code} className="catalog-chip-filter"><legend>{facet.name}</legend><div>{values.map((value) => <button type="button" key={value} className={compatibleAttributeFilters[code] === value ? "active" : ""} aria-pressed={compatibleAttributeFilters[code] === value} onClick={() => setAttributeFilters((current) => ({ ...current, [code]: current[code] === value ? "" : value }))}>{isBoolean ? (value === "true" ? "Да" : "Нет") : attributeLabel(value)}</button>)}</div></fieldset>;
    }
    return <label key={code}>{facet.name}{facet.unit ? `, ${facet.unit}` : ""}<select value={compatibleAttributeFilters[code] || ""} onChange={(event) => setAttributeFilters((current) => ({ ...current, [code]: event.target.value }))}><option value="">Любое значение</option>{values.map((value) => <option key={value} value={value}>{isBoolean ? (value === "true" ? "Да" : "Нет") : attributeLabel(value)}</option>)}</select></label>;
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
  const skeleton = <div className="storefront-grid storefront-skeleton" aria-label="Загружаем товары" aria-busy="true">{Array.from({ length: 8 }, (_, index) => <div className="storefront-card ui-card" key={index}><span className="skeleton-image" /><span className="skeleton-line wide" /><span className="skeleton-line" /><span className="skeleton-line price" /></div>)}</div>;

  if (routePending) return <main className="storefront"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} query={query} onQueryChange={setQuery} onCartClick={() => window.location.assign("/cart")} homeNavigation catalogMenuItems={headerMenus.catalog} plantMenuItems={headerMenus.plants} /><div className="ui-container route-skeleton">{skeleton}</div></main>;

  return (
    <main className="storefront">
      <StoreHeader cartCount={cartCount} favoritesCount={favorites.size} query={query} onQueryChange={setQuery} onCartClick={() => window.location.assign("/cart")} homeNavigation catalogMenuItems={headerMenus.catalog} plantMenuItems={headerMenus.plants} />

      {landing ? <section className="catalog-landing-hero ui-container"><nav aria-label="Хлебные крошки"><a href="/">Главная</a><span>/</span><a href="/#catalog">Каталог</a></nav><h1>{landingTitle}</h1><p>{landingDescription}</p></section> : <section className="home-hero ui-container" aria-labelledby="home-title">
        <div className="home-hero-copy"><h1 id="home-title">Растения, с которыми <em>хорошо.</em></h1><p>Живые растения для дома и офиса. Выбираем лучшее и доставляем по всей России.</p><a className="primary-button" href="#catalog">Выбрать растение <span aria-hidden="true">→</span></a></div>
        <div className="home-hero-visual"><img src="/assets/redesign/home-hero-4k.webp" alt="Алоказия в керамическом кашпо" /><span className="home-stamp ui-badge">Бережная доставка по России</span></div>
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
            <button type="button" className="secondary-button home-filter-button" aria-expanded={filtersOpen} onClick={() => setFiltersOpen((value) => !value)}>Фильтры{activeFilterCount > 0 && <b>{activeFilterCount}</b>}</button>
            <label className="storefront-check"><input type="checkbox" checked={inStockOnly} onChange={(event) => setInStockOnly(event.target.checked)} />Только в наличии</label>
            <label className="home-sort"><span>Сортировка</span><select aria-label="Сортировка" value={sort} onChange={(event) => setSort(event.target.value)}><option value="popular">По популярности</option><option value="cheap">Сначала дешевле</option><option value="expensive">Сначала дороже</option></select></label>
          </div>
          {activeFilterChips.length > 0 && <div className="active-filter-chips" aria-label="Активные фильтры">{activeFilterChips.map((chip) => <button type="button" key={chip.key} onClick={chip.remove} aria-label={`Удалить фильтр: ${chip.label}`}>{chip.label}<span aria-hidden="true">×</span></button>)}<button type="button" className="clear-filters" onClick={clearAdditionalFilters}>Сбросить все</button></div>}
          {filtersOpen && facets.length > 0 && <div className="home-filter-panel"><div className="storefront-attribute-filters">{facets.map(renderFacet)}</div></div>}

          {loading && skeleton}
          {!loading && error && <div className="storefront-empty" role="alert"><strong>Не удалось загрузить товары</strong><p>{error}. Попробуйте обновить страницу.</p></div>}
          {!loading && !error && visible.length === 0 && <div className="storefront-empty"><strong>{searching ? "Ничего не нашли" : "Здесь пока пусто"}</strong><p>{searching ? "Проверьте написание или поищите короче — например, «фикус» вместо «фикус бенджамина большой»." : activeFilterCount > 0 ? "По текущим фильтрам товаров нет. Сбросьте дополнительные фильтры или измените условия." : landing?.type === "collection" ? "В этой подборке сейчас нет доступных товаров." : "В этой категории сейчас нет доступных товаров."}</p>{activeFilterCount > 0 ? <button type="button" onClick={clearAdditionalFilters}>Сбросить фильтры</button> : <a href="/#catalog">Показать весь каталог</a>}</div>}

          <div className="storefront-grid">
            {visible.slice(0, visibleLimit).map((product) => {
              const inCart = cart[product.sku] ?? 0;
              const preorder = (product.stock ?? 0) <= 0;
              return <article key={product.id} className={preorder ? "storefront-card ui-card preorder" : "storefront-card ui-card"}>
                <button className={favorites.has(product.id) ? "storefront-fav active" : "storefront-fav"} onClick={() => toggleFavorite(product.id)} aria-label="В избранное">♥</button>
                <a className="storefront-image" href={`/product/${product.id}`} onClick={() => track("select_item", { productCode: product.id, sku: product.sku, value: product.price, properties: { list: landing ? landingTitle : categoryName || "catalog" } })}>{product.image ? <img src={product.image} alt={product.name} loading="lazy" onError={(event) => { event.currentTarget.hidden = true; event.currentTarget.nextElementSibling?.removeAttribute("hidden"); }} /> : null}<span className="storefront-image-fallback" hidden={Boolean(product.image)} aria-hidden="true">Нет фотографии</span></a>
                <a className="storefront-name" href={`/product/${product.id}`} onClick={() => track("select_item", { productCode: product.id, sku: product.sku, value: product.price, properties: { list: landing ? landingTitle : categoryName || "catalog" } })}>{product.name}</a>
                {product.filterAttributes?.some((attribute) => attribute.badge) && <div className="storefront-attribute-badges">{product.filterAttributes.filter((attribute) => attribute.badge).slice(0, 2).map((attribute) => <span className="ui-badge" key={attribute.code}>{attribute.name}: {attributeValue(attribute.value, attribute.unit)}</span>)}</div>}
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
