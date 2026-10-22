import { useEffect, useMemo, useState } from "react";
import CheckoutHost from "./CheckoutHost";
import { StoreHeader, type HeaderMenuItem } from "./StoreHeader";
import { CollectionStrip } from "./Collections";
import NotFoundPage from "./NotFoundPage";
import { searchProducts } from "./lib/search";
import { useSharedCart } from "./lib/cart";
import { track } from "./lib/analytics";
import { attributeLabel } from "./product/types";
import { CatalogProductCard } from "./CatalogProductCard";
import { sharedAPIJSON } from "./lib/api";
import { useFavorites } from "./lib/favorites";
import { counted } from "./lib/storefront-format";
import { orderStorefrontCategories } from "./lib/category-order";

type FilterDisplayMode = "select" | "chips" | "range";
type FilterDataType = "text" | "number" | "boolean" | "enum" | "multi_enum";
type FilterAttribute = {
  code: string;
  name: string;
  unit?: string;
  value: string | number | boolean | string[];
  displayValue?: string | number | string[];
  options?: string[];
  optionLabels?: Record<string, string>;
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
  heightCm?: number;
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
type CollectionMeta = { slug: string; title: string; note: string; count?: number; selectsVariant?: boolean };
type RangeValue = { min: string; max: string };
type Facet = {
  name: string;
  unit?: string;
  values: Set<string>;
  labels: Map<string, string>;
  displayMode: FilterDisplayMode;
  dataType?: FilterDataType;
  numericValues: number[];
};

const primaryFacetCodes = ["light_level", "care_level", "flowering", "pot_diameter_cm", "height_cm", "watering"];
const facetTitles: Record<string, string> = { light_level: "Уровень освещения", care_level: "Уровень ухода", flowering: "Цветение", pot_diameter_cm: "Горшок, см", height_cm: "Высота", watering: "Полив" };
const heightPresets = [
  { min: "", max: "30", label: "До 30 см" },
  { min: "30", max: "60", label: "30–60 см" },
  { min: "60", max: "100", label: "60–100 см" },
  { min: "100", max: "150", label: "100–150 см" },
  { min: "150", max: "", label: "От 150 см" },
];
const pricePresets = [
  { min: "", max: "1000", label: "До 1 000" },
  { min: "", max: "3000", label: "До 3 000" },
  { min: "", max: "5000", label: "До 5 000" },
  { min: "5000.01", max: "", label: "Больше 5 000" },
];

function facetValueLabel(code: string, facet: Facet, value: string): string {
  if (code === "flowering") return value === "true" ? "Цветущее" : "Не цветущее";
  if (code === "care_level") return ({ easy: "Лёгкий", medium: "Средний", demanding: "Сложный" } as Record<string, string>)[value] || attributeLabel(value);
  if (value === "true" || value === "false") return value === "true" ? "Есть" : "Нет";
  return facet.labels.get(value) || attributeLabel(value);
}

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
    <summary aria-label={selected || label}>{className.includes("home-sort") && <svg className="catalog-sort-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M6 4v16m-3-3 3 3 3-3M14 4h7M14 9h5M14 14h3" /></svg>}<span>{selected || label}</span><i>⌄</i></summary>
    <div role="listbox" aria-label={label}>
      {className !== "home-sort" && <button type="button" className={!value ? "active" : ""} onClick={(event) => { onChange(""); event.currentTarget.closest("details")?.removeAttribute("open"); }}>Все варианты</button>}
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

function attributeDisplayLabels(attribute: FilterAttribute): Map<string, string> {
  const labels = new Map<string, string>();
  Object.entries(attribute.optionLabels || {}).forEach(([value, label]) => { if (label) labels.set(value, label); });
  if (attribute.displayValue !== undefined && attribute.displayValue !== null) {
    const values = attributeValues(attribute).map(String);
    const displayed = (Array.isArray(attribute.displayValue) ? attribute.displayValue : [attribute.displayValue]).map(String);
    if (values.length === displayed.length) values.forEach((value, index) => { if (displayed[index]) labels.set(value, displayed[index]); });
    else if (values.length === 1 && displayed.length) labels.set(values[0], displayed.join(", "));
  }
  return labels;
}

export default function StorefrontPage({ landing }: { landing?: Landing }) {
  const [products, setProducts] = useState<Product[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [categoriesLoaded, setCategoriesLoaded] = useState(false);
  const [collectionMeta, setCollectionMeta] = useState<CollectionMeta | null>(null);
  const [collectionVariants, setCollectionVariants] = useState<{ slug: string; products: Product[]; failed: boolean } | null>(null);
  const [collectionState, setCollectionState] = useState<"idle" | "loading" | "loaded" | "error">(landing?.type === "collection" ? "loading" : "idle");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState(() => new URLSearchParams(window.location.search).get("q") ?? "");
  const [searchDraft, setSearchDraft] = useState(query);
  const [homeCategory, setHomeCategory] = useState(() => new URLSearchParams(window.location.search).get("category") || "");

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

  const [cart, setCart, cartMeta] = useSharedCart();
  const [favorites, setFavorites] = useFavorites();
  const [cartOpen, setCartOpen] = useState(false);

  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.has("cart")) window.location.replace("/cart");
  }, []);

  useEffect(() => {
    sharedAPIJSON<{ products?: Product[] }>("/api/v1/catalog")
      .then((data: { products?: Product[] }) => {
        setProducts(data.products ?? []);
        setError(data.products?.length ? "" : "Каталог пока пуст");
      })
      .catch(() => setError("Каталог временно недоступен"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    sharedAPIJSON<{ categories?: Category[] }>("/api/v1/categories")
      .then((data: { categories?: Category[] }) => setCategories(data.categories ?? []))
      .catch(() => setCategories([]))
      .finally(() => setCategoriesLoaded(true));
  }, []);

  useEffect(() => {
    if (landing?.type !== "collection" || !collectionMeta?.selectsVariant) return;
    let alive = true;
    const slug = landing.slug;
    sharedAPIJSON<{ products?: Product[] }>(`/api/v1/catalog?collection=${encodeURIComponent(slug)}`)
      .then(data => { if (alive) setCollectionVariants({ slug, products: data.products || [], failed: false }); })
      .catch(() => { if (alive) setCollectionVariants({ slug, products: [], failed: true }); });
    return () => { alive = false; };
  }, [landing, collectionMeta?.selectsVariant]);

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
    () => categories.find((item) => landing?.type === "category" ? item.slug === landing.slug : !landing && String(item.id) === homeCategory) ?? null,
    [categories, landing, homeCategory],
  );
  const category = activeCategory?.id ?? null;
  const categoryName = activeCategory?.name ?? "";
  const searching = query.trim().length > 0;
  const updateSearch = (next: string) => {
    setSearchDraft(next);

  };
  const submitSearch = (next: string) => {
    setQuery(next);
    setSearchDraft(next);
    window.scrollTo({top:0,behavior:"instant"});
  };
  const cancelSearch = () => { setSearchDraft(query); };
  const categoryNotFound = landing?.type === "category" && categoriesLoaded && !activeCategory;
  const collectionNotFound = landing?.type === "collection" && collectionState === "loaded" && !collectionMeta;
  const routePending = landing?.type === "category"
    ? !categoriesLoaded
    : landing?.type === "collection"
      ? collectionState === "loading" || (collectionMeta?.selectsVariant && collectionVariants?.slug !== landing.slug)
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
    const order = orderStorefrontCategories<Category>;
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

  const categoryOptions = useMemo(() => {
    const collect = (nodes: CategoryNode[], depth = 0): Array<{ slug: string; label: string }> => nodes.flatMap((node) => node.count > 0
      ? [{ slug: node.slug, label: `${"  ".repeat(depth)}${node.name} · ${node.count}` }, ...collect(node.children, depth + 1)]
      : []);
    return collect(tree);
  }, [tree]);

  const headerMenus = useMemo(() => {
    const children = new Map<number | null, Category[]>();
    categories.forEach((item) => children.set(item.parentId, [...(children.get(item.parentId) || []), item]));
    const order = orderStorefrontCategories<Category>;
    const branch = (item: Category): HeaderMenuItem => ({id:item.id,label:item.name,slug:item.slug,children:order(children.get(item.id) || []).map(branch)});
    const roots = order(children.get(null) || []).map(branch);
    const plantRoot = categories.find((item) => item.parentId == null && /растен/i.test(item.name));
    if (!plantRoot) return { catalog: roots, plants: [] };
    const collectPlantKinds = (parentId: number): Category[] => order(children.get(parentId) || []).flatMap((item): Category[] => {
      const nested = children.get(item.id) || [];
      return nested.length ? collectPlantKinds(item.id) : [item];
    });
    return { catalog: roots, plants: order(collectPlantKinds(plantRoot.id)).map((item) => ({ id: item.id, label: item.name, slug: item.slug })) };
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
    if (landing?.type === "collection") {
      const source = collectionMeta?.selectsVariant
        ? collectionVariants?.slug === landing.slug ? collectionVariants.products : []
        : products;
      return source.filter(product => product.collections?.includes(landing.slug));
    }
    if (landing?.type === "category" && !activeCategory) return [];
    if (category != null) return products.filter((product) => inBranch(product.categoryId, category));
    return products;
  }, [products, landing, activeCategory, category, inBranch, collectionMeta?.selectsVariant, collectionVariants]);

  // This is the existing documented search rule: search is global even when
  // started from a category or collection. We keep it and explain the scope in
  // the UI rather than silently inventing a different product rule.
  const found = useMemo(() => searching ? searchProducts(products, query) : contextProducts, [products, contextProducts, query, searching]);

  const facetPopulation = searching ? found : contextProducts;
  const [priceRange, setPriceRange] = useState<RangeValue>(() => {
    const params = new URLSearchParams(window.location.search);
    return { min: params.get("priceMin") || "", max: params.get("priceMax") || "" };
  });

  useEffect(() => {
    const restoreLocation = () => {
      const params = new URLSearchParams(window.location.search);
      setHomeCategory(params.get("category") || "");
      setQuery(params.get("q") || "");
      setSearchDraft(params.get("q") || "");
      setInStockOnly(params.get("stock") === "1");
      setSort(params.get("sort") || "popular");
      setPriceRange({ min: params.get("priceMin") || "", max: params.get("priceMax") || "" });
      setAttributeFilters(initialAttributeFilters());
      setRangeFilters(initialRangeFilters());
    };
    window.addEventListener("popstate", restoreLocation);
    return () => window.removeEventListener("popstate", restoreLocation);
  }, []);
  const facets = useMemo(() => {
    const result = new Map<string, Facet>();
    facetPopulation.forEach((product) => product.filterAttributes?.filter((attribute) => attribute.filterable).forEach((attribute) => {
      const mode = inferredDisplayMode(attribute);
      const facet = result.get(attribute.code) || {
        name: attribute.name,
        unit: attribute.unit,
        values: new Set<string>(),
        labels: new Map<string, string>(),
        displayMode: mode,
        dataType: attribute.dataType,
        numericValues: [],
      };
      attributeValues(attribute).forEach((value) => {
        if (value === null || value === undefined || String(value) === "") return;
        facet.values.add(String(value));
        if (typeof value === "number" || (mode === "range" && Number.isFinite(Number(value)))) facet.numericValues.push(Number(value));
      });
      attributeDisplayLabels(attribute).forEach((label, value) => facet.labels.set(value, label));
      result.set(attribute.code, facet);
    }));
    // A mixed catalogue only shares facets represented in every product section.
    // A plants-only home/search gets the same useful filters as its category.
    const sections = new Set(facetPopulation.map((product) => product.catalogSection));
    return [...result.entries()].filter(([code, facet]) => facet.values.size > 0 && [...sections].every((section) => facetPopulation.some((product) => product.catalogSection === section && product.filterAttributes?.some((attribute) => attribute.code === code && attribute.filterable))));
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

  // Keep the requested buyer criteria together, even in a category with one value.
  const hasPlantFacets = facetPopulation.some(product => product.catalogSection === "plants");
  const primaryFacets = facets.filter(([code, facet]) => primaryFacetCodes.includes(code) || (!hasPlantFacets && facet.values.size > 1)).sort(([a], [b]) => primaryFacetCodes.indexOf(a) - primaryFacetCodes.indexOf(b));

  useEffect(() => {
    const url = new URL(window.location.href);
    const managed: string[] = [];
    url.searchParams.forEach((_value, key) => {
      if (key === "q" || key === "stock" || key === "sort" || key === "priceMin" || key === "priceMax" || key.startsWith("filter.") || key.startsWith("min.") || key.startsWith("max.")) managed.push(key);
    });
    managed.forEach((key) => url.searchParams.delete(key));
    if (query.trim()) url.searchParams.set("q", query.trim());
    if (inStockOnly) url.searchParams.set("stock", "1");
    if (priceRange.min) url.searchParams.set("priceMin", priceRange.min);
    if (priceRange.max) url.searchParams.set("priceMax", priceRange.max);
    if (sort !== "popular") url.searchParams.set("sort", sort);
    Object.entries(compatibleAttributeFilters).forEach(([code, value]) => { if (value) url.searchParams.set(`filter.${code}`, value); });
    Object.entries(compatibleRangeFilters).forEach(([code, value]) => {
      if (value.min) url.searchParams.set(`min.${code}`, value.min);
      if (value.max) url.searchParams.set(`max.${code}`, value.max);
    });
    window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  }, [query, inStockOnly, sort, priceRange, compatibleAttributeFilters, compatibleRangeFilters]);

  const activeFilterCount = useMemo(() => {
    const attributes = Object.values(compatibleAttributeFilters).filter(Boolean).length;
    const ranges = Object.values(compatibleRangeFilters).filter((value) => value.min || value.max).length;
    return attributes + ranges + (inStockOnly ? 1 : 0) + (priceRange.min || priceRange.max ? 1 : 0);
  }, [compatibleAttributeFilters, compatibleRangeFilters, inStockOnly, priceRange]);

  const visible = useMemo(() => {
    let list = found;
    if (priceRange.min) list = list.filter((item) => item.price >= Number(priceRange.min));
    if (priceRange.max) list = list.filter((item) => item.price <= Number(priceRange.max));
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
    if (sort === "popular") list = [...list].sort((a, b) => (b.popularityScore ?? 0) - (a.popularityScore ?? 0) || Number((b.stock ?? 0) > 0) - Number((a.stock ?? 0) > 0));
    return list;
  }, [found, inStockOnly, compatibleAttributeFilters, compatibleRangeFilters, sort, priceRange]);

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
    setPriceRange({ min: "", max: "" });
  };

  const pickHomeCategory = (id: number | null) => {
    if (landing) return;
    const url = new URL(window.location.href);
    if (id == null) url.searchParams.delete("category");
    else url.searchParams.set("category", String(id));
    url.searchParams.delete("q");
    window.history.pushState(null, "", `${url.pathname}${url.search}${url.hash}`);
    setHomeCategory(id == null ? "" : String(id));
    setQuery("");
    setSearchDraft("");
  };
  const categoryClick = (event: React.MouseEvent<HTMLAnchorElement>, id: number | null) => {
    if (landing || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    pickHomeCategory(id);
  };

  const appliedFilters: Array<{ label: string; remove: () => void }> = [];
  if (inStockOnly) appliedFilters.push({ label: "В наличии", remove: () => setInStockOnly(false) });
  if (priceRange.min || priceRange.max) appliedFilters.push({ label: `Цена: ${priceRange.min ? `от ${Number(priceRange.min).toLocaleString("ru-RU")}` : ""}${priceRange.min && priceRange.max ? " " : ""}${priceRange.max ? `до ${Number(priceRange.max).toLocaleString("ru-RU")}` : ""} ₽`, remove: () => setPriceRange({ min: "", max: "" }) });
  facets.forEach(([code, facet]) => {
    const selected = compatibleAttributeFilters[code];
    if (selected) appliedFilters.push({ label: `${facet.name}: ${facetValueLabel(code, facet, selected)}`, remove: () => setAttributeFilters((current) => ({ ...current, [code]: "" })) });
    const range = compatibleRangeFilters[code];
    if (range?.min || range?.max) appliedFilters.push({ label: `${facet.name}: ${range.min ? `от ${range.min}` : ""}${range.min && range.max ? " " : ""}${range.max ? `до ${range.max}` : ""}${facet.unit ? ` ${facet.unit}` : ""}`, remove: () => setRangeFilters((current) => ({ ...current, [code]: { min: "", max: "" } })) });
  });

  const toggleCategory = (id: number) => setOpened((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });

  const branch = (node: CategoryNode, depth: number) => {
    const persistentRoot = depth === 0 && tree.length === 1;
    const expanded = persistentRoot || opened.has(node.id) || activeCategoryPath.has(node.id);
    return <div key={node.id}>
      <a href={`/catalog/${encodeURIComponent(node.slug)}`} onClick={event => categoryClick(event, node.id)} className={category === node.id ? "active" : ""} style={{ paddingLeft: 10 + depth * 14 }} aria-current={category === node.id ? "page" : undefined}>
        <span>{node.children.length > 0 && !persistentRoot && <i className={expanded ? "twist open" : "twist"} aria-hidden="true" onClick={(event) => { event.preventDefault(); event.stopPropagation(); toggleCategory(node.id); }}>›</i>}<CategoryIcon name={node.icon} />{node.name}</span>
        <small>{node.count}</small>
      </a>
      {expanded && node.children.map((child) => branch(child, depth + 1))}
    </div>;
  };

  const renderFacet = ([code, facet]: [string, Facet]) => {
    const label = facetTitles[code] || facet.name;
    if (code === "height_cm") {
      const selected = compatibleRangeFilters[code] || { min: "", max: "" };
      const value = selected.min || selected.max ? `${selected.min}:${selected.max}` : "";
      const custom = value && !heightPresets.some(preset => `${preset.min}:${preset.max}` === value);
      return <label key={code}>{label}<select aria-label={label} value={value} onChange={event => {
        const [min = "", max = ""] = event.target.value.split(":");
        setRangeFilters(current => ({ ...current, [code]: { min, max } }));
      }}><option value="">Любая</option>{heightPresets.map(preset => <option key={preset.label} value={`${preset.min}:${preset.max}`}>{preset.label}</option>)}{custom && <option value={value}>{selected.min ? `От ${selected.min}` : ""}{selected.min && selected.max ? " " : ""}{selected.max ? `до ${selected.max}` : ""} см</option>}</select></label>;
    }
    const values = [...facet.values].sort((a, b) => code === "care_level" ? ["easy", "medium", "demanding"].indexOf(a) - ["easy", "medium", "demanding"].indexOf(b) : a.localeCompare(b, "ru", { numeric: true }));
    return <label key={code}>{label}<select aria-label={label} value={compatibleAttributeFilters[code] || ""} onChange={event => setAttributeFilters(current => ({ ...current, [code]: event.target.value }))}><option value="">Любое значение</option>{values.map(value => <option key={value} value={value}>{facetValueLabel(code, facet, value)}</option>)}</select></label>;
  };

  const cartCount = Object.values(cart).reduce((sum, value) => sum + value, 0);
  const addToCart = (product: Product) => {
    track("add_to_cart", { productCode: product.id, sku: product.sku, value: product.price, quantity: 1, properties: { name: product.name, category: product.catalogSection, list: landing ? landingTitle : categoryName || "catalog" } });
    setCart((current) => ({ ...current, [product.sku]: Math.min(product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20, (current[product.sku] ?? 0) + 1) }));
  };
  const toggleFavorite = (id: string) => setFavorites((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id); else next.add(id);
    return next;
  });

  if (categoryNotFound || collectionNotFound) return <NotFoundPage />;
  const collectionVariantError = !searching && landing?.type === "collection" && collectionVariants?.slug === landing.slug && collectionVariants.failed;
  const CatalogHeading = landing ? "h1" : "h2";
  const showCategoryPicker = !searching && (!landing || landing.type === "category") && categoryOptions.length > 0;

  if (routePending) return <main className={`storefront${searching ? " searching" : ""}`}><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} query={searchDraft} onQueryChange={updateSearch} onSearch={submitSearch} onSearchClose={cancelSearch} onCartClick={() => window.location.assign("/cart")} homeNavigation onHomeCategoryPick={!landing ? (id) => { pickHomeCategory(id); document.getElementById("catalog")?.scrollIntoView({ behavior: "instant" }); } : undefined} catalogMenuItems={headerMenus.catalog} plantMenuItems={headerMenus.plants} /><p className="storefront-empty">Загружаем каталог…</p></main>;

  return (
    <main className={`storefront${searching ? " searching" : ""}`}>
      <StoreHeader cartCount={cartCount} favoritesCount={favorites.size} query={searchDraft} onQueryChange={updateSearch} onSearch={submitSearch} onSearchClose={cancelSearch} onCartClick={() => window.location.assign("/cart")} homeNavigation onHomeCategoryPick={!landing ? (id) => { pickHomeCategory(id); document.getElementById("catalog")?.scrollIntoView({ behavior: "instant" }); } : undefined} catalogMenuItems={headerMenus.catalog} plantMenuItems={headerMenus.plants} />

      {!searching && !landing && (<section className="home-hero" aria-labelledby="home-title">
        <div className="home-hero-copy"><h1 id="home-title">Растения,<br />с которыми<br /><em>хорошо</em><i>.</i></h1><p>Живые растения для дома и офиса.<br />Выбираем лучшее и доставляем по всей России.</p><div className="home-hero-actions"><a href="#catalog">Выбрать своё растение <span>→</span></a></div><div className="home-team"><img src="/assets/redesign/team-avatars.webp" alt="Команда Фикусин" /><span>За вашими растениями<br />ухаживает <b>команда Фикусина</b></span></div></div>
        <div className="home-hero-visual"><img src="/assets/redesign/home-hero-4k.webp" alt="Алоказия в керамическом кашпо" /><span className="home-stamp" role="img" aria-label="Живые растения для живых людей"><svg viewBox="0 0 132 132" aria-hidden="true"><defs><path id="stamp-top" d="M25 64 A41 41 0 0 1 107 64" /><path id="stamp-bottom" d="M25 74 A41 41 0 0 0 107 74" /></defs><circle cx="66" cy="66" r="58" /><circle cx="66" cy="66" r="52" strokeDasharray="2 4" /><text><textPath href="#stamp-top" startOffset="50%" textAnchor="middle">ЖИВЫЕ РАСТЕНИЯ</textPath></text><text><textPath href="#stamp-bottom" startOffset="50%" textAnchor="middle">ДЛЯ ЖИВЫХ ЛЮДЕЙ</textPath></text><path className="stamp-plant" d="M66 82V55m0 10c-12 0-16-8-16-14 9 0 16 4 16 14Zm0 7c12 0 16-8 16-14-9 0-16 4-16 14ZM55 85h22" /></svg></span><span className="home-note delivery"><svg className="note-icon" viewBox="0 0 48 58" aria-hidden="true"><path d="M9 24 24 16l15 8v25L24 56 9 49V24Zm15-8v40m-15-32 15 8 15-8M24 16V3m0 9c-8 0-11-5-11-10 7 0 11 3 11 10Zm0-2c7 0 10-5 10-9-6 0-10 3-10 9Z" /></svg><b>Доставка<br />по всей России</b></span><a className="home-note packing" href="/delivery-and-returns"><svg className="note-icon" viewBox="0 0 48 58" aria-hidden="true"><path d="M9 24 24 16l15 8v25L24 56 9 49V24Zm15-8v40m-15-32 15 8 15-8M24 16V3m0 9c-8 0-11-5-11-10 7 0 11 3 11 10Zm0-2c7 0 10-5 10-9-6 0-10 3-10 9Z" /></svg><b>Как упаковываем<br />и доставляем растения</b><i aria-hidden="true">→</i></a></div>
      </section>)}

      {!searching && landing?.type !== "category" && <CollectionStrip products={products} activeSlug={landing?.type === "collection" ? landing.slug : undefined} />}

      <section className={`storefront-shell${showCategoryPicker ? " has-category-picker" : ""}`} id="catalog">
        <aside className="storefront-side">
          {!searching && <nav className="storefront-tree" aria-label="Категории товаров">
            <a href="/#catalog" onClick={event => categoryClick(event, null)} className={!landing && category == null ? "active" : ""} aria-current={!landing && category == null ? "page" : undefined}><span>Весь каталог</span>{!loading && <small>{products.length}</small>}</a>
            {tree.map((root) => branch(root, 0))}
          </nav>}

        </aside>

        <div className="storefront-main">
          <div className="storefront-head"><div><CatalogHeading>{searching ? "Результаты поиска" : landing ? landingTitle : categoryName || "Каталог"}</CatalogHeading><p>{loading ? "Загружаем товары…" : searching ? `Найдено: ${counted(visible.length, ["товар", "товара", "товаров"])}` : counted(visible.length, ["товар", "товара", "товаров"])}{!loading && searching && <span> по запросу «{query.trim()}»</span>}</p>{!searching && landing?.type === "collection" && <p className="catalog-collection-note">{landingDescription}</p>}{searching && landing && <p className="catalog-search-scope">Поиск выполняется по всему каталогу, без ограничения текущей {landing.type === "category" ? "категорией" : "подборкой"}.</p>}</div></div>



          <div className="home-catalog-toolbar">
            <button type="button" className={filtersOpen ? "home-filter-button active" : "home-filter-button"} aria-expanded={filtersOpen} aria-controls="catalog-filters" aria-label="Фильтры" onClick={() => setFiltersOpen((value) => !value)}><svg className="filter-sliders" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16M8 3v6M4 12h16M16 9v6M4 18h16M10 15v6" /></svg><span className="catalog-tool-label">Фильтры</span><i>⌄</i>{activeFilterCount > 0 && <b>{activeFilterCount}</b>}</button>
            <CatalogDropdown className="home-sort" label="По популярности" value={sort} onChange={setSort} options={[{ value: "popular", label: "По популярности" }, { value: "cheap", label: "Сначала дешевле" }, { value: "expensive", label: "Сначала дороже" }]} />
            <div className="catalog-view-toggle" aria-label="Вид каталога"><button type="button" className={viewMode === "grid" ? "active" : ""} onClick={() => setViewMode("grid")} aria-label="Плитка" aria-pressed={viewMode === "grid"}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z" /></svg></button><button type="button" className={viewMode === "list" ? "active" : ""} onClick={() => setViewMode("list")} aria-label="Список" aria-pressed={viewMode === "list"}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 5h16M4 12h16M4 19h16" /></svg></button></div>
          </div>
          <div className="home-filter-panel" id="catalog-filters" hidden={!filtersOpen}>
            <div className="storefront-attribute-filters">
          {showCategoryPicker && <label className="catalog-category-picker">Категория
            <select aria-label="Категория" className={activeCategory ? "has-selection" : undefined} value={activeCategory?.slug || ""} onChange={(event) => { if (!landing) pickHomeCategory(categories.find(item => item.slug === event.target.value)?.id ?? null); else window.location.assign(event.target.value ? `/catalog/${encodeURIComponent(event.target.value)}` : "/#catalog"); }}>
              <option value="">Весь каталог · {products.length}</option>
              {activeCategory && !categoryOptions.some((option) => option.slug === activeCategory.slug) && <option value={activeCategory.slug}>{activeCategory.name} · {contextProducts.length}</option>}
              {categoryOptions.map((option) => <option value={option.slug} key={option.slug}>{option.label}</option>)}
            </select>
          </label>}
              <div className="catalog-light-filter">{primaryFacets.filter(([code]) => code === "light_level").map(renderFacet)}<label className="catalog-stock-switch"><input type="checkbox" role="switch" checked={inStockOnly} onChange={event => setInStockOnly(event.target.checked)} /><span aria-hidden="true" />Только в наличии</label></div>
              {primaryFacets.filter(([code]) => code !== "light_level").map(renderFacet)}
            </div>
            <div className="catalog-budget-row">
              <fieldset className="catalog-budget"><legend>Цена, ₽</legend><div className="catalog-budget-controls">
                <input aria-label="Цена: от" placeholder="От" type="number" min="0" inputMode="decimal" value={priceRange.min} onChange={event => setPriceRange(current => ({ ...current, min: event.target.value }))} />
                <span aria-hidden="true">—</span>
                <input aria-label="Цена: до" placeholder="До" type="number" min="0" inputMode="decimal" value={priceRange.max} onChange={event => setPriceRange(current => ({ ...current, max: event.target.value }))} />
                <div className="catalog-price-presets" aria-label="Быстрый выбор цены">{pricePresets.map(preset => <button key={preset.label} type="button" aria-pressed={priceRange.min === preset.min && priceRange.max === preset.max} onClick={() => setPriceRange({ min: preset.min, max: preset.max })}>{preset.label}</button>)}</div>
              </div></fieldset>
            </div>
          </div>
          {appliedFilters.length > 0 && <div className="catalog-applied-filters" aria-label="Выбранные фильтры">{appliedFilters.map((filter) => <button type="button" key={filter.label} aria-label={`Убрать фильтр: ${filter.label}`} onClick={filter.remove}>{filter.label}<svg viewBox="0 0 16 16" aria-hidden="true"><path d="m4 4 8 8M12 4l-8 8" /></svg></button>)}<button type="button" onClick={clearAdditionalFilters}>Сбросить фильтры</button></div>}

          {loading && <p className="storefront-empty">Загружаем каталог…</p>}
          {!loading && error && <p className="storefront-empty">{error}</p>}
          {collectionVariantError && <p className="storefront-empty" role="alert">Не удалось загрузить размеры подборки. Обновите страницу, чтобы повторить загрузку.</p>}
          {!loading && !error && !collectionVariantError && visible.length === 0 && <div className="storefront-empty"><strong>{searching ? "Ничего не нашли" : "Здесь пока пусто"}</strong><p>{searching ? "Проверьте написание или поищите короче — например, «фикус» вместо «фикус бенджамина большой»." : activeFilterCount > 0 ? "По текущим фильтрам товаров нет. Сбросьте дополнительные фильтры или измените условия." : landing?.type === "collection" ? "В этой подборке сейчас нет доступных товаров." : "В этой категории сейчас нет доступных товаров."}</p>{activeFilterCount > 0 ? <button type="button" onClick={clearAdditionalFilters}>Сбросить фильтры</button> : <a href="/#catalog">Показать весь каталог</a>}</div>}

          <div className={`storefront-grid ${viewMode === "list" ? "list-view" : ""}`}>
            {visible.slice(0, visibleLimit).map(product => <CatalogProductCard key={product.id} product={product} preserveSKU={!searching && landing?.type === "collection" && collectionMeta?.selectsVariant}
              inCart={cart[product.sku] ?? 0} favorite={favorites.has(product.id)}
              onFavorite={() => toggleFavorite(product.id)} onAdd={() => addToCart(product)} onDecrease={() => setCart(current => {
                const next = { ...current };
                if ((next[product.sku] || 0) <= 1) delete next[product.sku]; else next[product.sku] -= 1;
                return next;
              })}
              onSelect={() => track("select_item", { productCode: product.id, sku: product.sku, value: product.price, properties: { list: landing ? landingTitle : categoryName || "catalog" } })} />)}
          </div>
          {visible.length > visibleLimit && <button className="storefront-more" type="button" onClick={() => setVisibleLimit((value) => value + 12)}>Показать ещё растения <span>⌄</span></button>}
        </div>
      </section>
      <CheckoutHost cart={cart} products={[...products, ...cartMeta.lines.filter((line) => !products.some((product) => product.sku === line.sku))]} cartOpen={cartOpen} onCartOpenChange={setCartOpen} onCartChange={setCart}
        cartStatus={cartMeta.status} cartError={cartMeta.error} missingCartItems={cartMeta.missingSkus.length} onCartRetry={cartMeta.retry} />
    </main>
  );
}
