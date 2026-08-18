import { useEffect, useMemo, useState } from "react";
import CheckoutHost from "./CheckoutHost";
import { StoreHeader, type HeaderMenuItem } from "./StoreHeader";
import { CollectionStrip, presets } from "./Collections";
import { searchProducts } from "./lib/search";
import { STORAGE_EVENT } from "./StoreHeader";
import { attributeLabel, attributeValue } from "./product/types";

type Product = {
  id: string;
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
  rating: number; reviewsCount: number;
  popularityScore?: number;
  filterAttributes?: Array<{ code: string; name: string; unit?: string; value: string | number | boolean | string[]; filterable: boolean; badge: boolean }>;
};

type Category = { id: number; parentId: number | null; name: string; slug: string; sortOrder: number; icon: string };
// Не Node: так называется узел DOM, и подмена ломает проверку клика мимо
// подсказок поиска.
type CategoryNode = { id: number; name: string; icon: string; count: number; children: CategoryNode[] };
type Cart = Record<string, number>;

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
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

// Витрина — это главная: товары с первого пикселя, поиск в липкой шапке,
// слева живое дерево каталога, над сеткой — подборки.
export default function StorefrontPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  // Запрос читается из адреса до первого рендера, иначе страница успела бы
  // нарисовать весь каталог и тут же его выбросить.
  const [query, setQuery] = useState(
    () => new URLSearchParams(window.location.search).get("q") ?? "",
  );
  const [category, setCategory] = useState<number | null>(() => {
    const value = Number(new URLSearchParams(window.location.search).get("category"));
    return Number.isInteger(value) && value > 0 ? value : null;
  });
  // На широком экране подбор раскрыт сразу, на телефоне — по нажатию.
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [opened, setOpened] = useState<Set<number>>(new Set());
  const [selectedPresets, setSelectedPresets] = useState<Set<string>>(new Set());
  const [inStockOnly, setInStockOnly] = useState(false);
  const [attributeFilters, setAttributeFilters] = useState<Record<string, string>>({});
  const [sort, setSort] = useState("popular");
  const [viewMode, setViewMode] = useState<"grid" | "list">("grid");
  const [visibleLimit, setVisibleLimit] = useState(12);
  useEffect(() => {
    const closeDropdowns = (event: PointerEvent) => document.querySelectorAll<HTMLDetailsElement>(".catalog-dropdown[open]").forEach((details) => {
      if (!details.contains(event.target as Node)) details.removeAttribute("open");
    });
    document.addEventListener("pointerdown", closeDropdowns);
    return () => document.removeEventListener("pointerdown", closeDropdowns);
  }, []);

  const [cart, setCart] = useState<Cart>(() => {
    try {
      const saved = window.localStorage.getItem("ficusin-cart");
      return saved ? (JSON.parse(saved) as Cart) : {};
    } catch {
      return {};
    }
  });
  const [favorites, setFavorites] = useState<Set<string>>(() => {
    try {
      return new Set(
        JSON.parse(window.localStorage.getItem("ficusin-favorites") || "[]") as string[],
      );
    } catch {
      return new Set();
    }
  });
  const [cartOpen, setCartOpen] = useState(
    () => new URLSearchParams(window.location.search).get("cart") === "1",
  );

  useEffect(() => {
    const url = new URL(window.location.href);
    if (!url.searchParams.has("cart")) return;
    url.searchParams.delete("cart");
    window.history.replaceState({}, "", `${url.pathname}${url.search}${url.hash}`);
  }, []);

  useEffect(() => {
    fetch("/api/v1/catalog", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { products?: Product[] }) => {
        setProducts(data.products ?? []);
        setError(data.products?.length ? "" : "Каталог пока пуст");
      })
      .catch(() => setError("Каталог временно недоступен"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    fetch("/api/v1/categories", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { categories?: Category[] }) => setCategories(data.categories ?? []))
      .catch(() => setCategories([]));
  }, []);

  useEffect(() => {
    window.localStorage.setItem("ficusin-cart", JSON.stringify(cart));
    window.dispatchEvent(new Event(STORAGE_EVENT));
  }, [cart]);

  const searching = query.trim().length > 0;

  // Дерево каталога строится из базы целиком, на любую глубину. Ступень с
  // единственной веткой и без собственных товаров ничего не решает:
  // «Растения → Комнатные растения → Фикус» заставляет нажать дважды, чтобы
  // увидеть ровно то же самое. Такую ступень пропускаем.
  // Заголовок страницы идёт за выбранной веткой каталога: покупатель,
  // пришедший по ссылке на «Аглаонемы», должен увидеть это словом.
  const activeFilterCount = useMemo(
    () => Object.values(attributeFilters).filter(Boolean).length + (inStockOnly ? 1 : 0),
    [attributeFilters, inStockOnly],
  );

  const categoryName = useMemo(
    () => categories.find((item) => item.id === category)?.name ?? "",
    [categories, category],
  );

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
    const order = (list: Category[]) =>
      [...list].sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name));

    const build = (item: Category): CategoryNode => {
      let children = order(kids.get(item.id) ?? []);
      while (children.length === 1 && (direct.get(children[0].id) ?? 0) === 0) {
        children = order(kids.get(children[0].id) ?? []);
      }
      const nodes = children.map(build);
      return {
        id: item.id,
        name: item.name,
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
    const order = (items: Category[]) => [...items].sort((a,b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name,"ru"));
    // «Каталог» показывает именно все корневые разделы. В «Растениях» сразу
    // показываем конечные виды, не заставляя покупателя открывать служебный
    // уровень «Комнатные растения».
    const roots: HeaderMenuItem[] = order(children.get(null) || []).map((item) => ({ id:item.id, label:item.name }));
    const plantRoot = categories.find((item) => item.parentId == null && /растен/i.test(item.name));
    if (!plantRoot) return { catalog: roots, plants: [] };
    const collectPlantKinds = (parentId: number): Category[] => order(children.get(parentId) || []).flatMap((item): Category[] => {
      const nested = children.get(item.id) || [];
      return nested.length ? collectPlantKinds(item.id) : [item];
    });
    const plants: HeaderMenuItem[] = collectPlantKinds(plantRoot.id).map((item) => ({ id:item.id, label:item.name }));
    return { catalog: roots, plants };
  }, [categories]);

  // Ветка считается выбранной вместе со всеми потомками, даже теми, что
  // пропущены при отрисовке.
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

  // Поиск сильнее любого фильтра: человек, стоящий в «Фикусах» и набравший
  // «монстера», имеет в виду растение, а не «в этой ветке ничего нет».
  const found = useMemo(
    () => (searching ? searchProducts(products, query) : products),
    [products, query, searching],
  );

  const visible = useMemo(() => {
    let list = found;
    if (!searching && category != null) {
      list = list.filter((item) => inBranch(item.categoryId, category));
    }
    if (selectedPresets.size > 0) {
      const rules = presets.filter((item) => selectedPresets.has(item.id));
      list = list.filter((product) => rules.every((rule) => rule.match(product)));
    }
    if (inStockOnly) list = list.filter((item) => (item.stock ?? 0) > 0);
    for (const [code, selected] of Object.entries(attributeFilters)) if (selected) list = list.filter((product) => {
      if (code === "__diameter") return product.size.match(/D\s*(\d+)/i)?.[1] === selected;
      if (code === "__light") return product.lightLevel === selected;
      if (code === "__watering") return product.watering === selected;
      if (code === "__care") return product.careLevel === selected;
      if (code === "__pets") return product.petSafety === selected;
      return product.filterAttributes?.some((attribute) => attribute.code === code && (Array.isArray(attribute.value) ? attribute.value.map(String).includes(selected) : String(attribute.value) === selected));
    });
    if (sort === "cheap") list = [...list].sort((a, b) => a.price - b.price);
    if (sort === "expensive") list = [...list].sort((a, b) => b.price - a.price);
    if (sort === "popular") list = [...list].sort((a, b) => (b.popularityScore ?? 0) - (a.popularityScore ?? 0));
    return list;
  }, [found, searching, category, inBranch, selectedPresets, inStockOnly, attributeFilters, sort]);

  const facets = useMemo(() => {
    const result = new Map<string, { name: string; unit?: string; values: Set<string> }>();
    products.forEach((product) => product.filterAttributes?.filter((attribute) => attribute.filterable).forEach((attribute) => {
      const facet = result.get(attribute.code) || { name: attribute.name, unit: attribute.unit, values: new Set<string>() };
      const values = Array.isArray(attribute.value) ? attribute.value : [attribute.value]; values.forEach((value) => facet.values.add(String(value))); result.set(attribute.code, facet);
    }));
    return [...result.entries()].filter(([, facet]) => facet.values.size > 1);
  }, [products]);

  // Четыре главных фильтра из утверждённого макета существуют независимо
  // от того, настроил ли менеджер дублирующие динамические характеристики.
  const catalogFacets = useMemo(() => {
    const values = (pick: (product: Product) => string | undefined) => new Set(products.map(pick).filter((value): value is string => Boolean(value)));
    return [
      ["__light", { name: "Освещённость", values: values((product) => product.lightLevel) }],
      ["__watering", { name: "Полив", values: values((product) => product.watering) }],
      ["__care", { name: "Уход", values: values((product) => product.careLevel) }],
      ["__diameter", { name: "Размеры", unit: "см", values: values((product) => product.size.match(/D\s*(\d+)/i)?.[1]) }],
      ["__pets", { name: "Для питомцев", values: values((product) => product.petSafety) }],
    ] as Array<[string, { name: string; unit?: string; values: Set<string> }]>;
  }, [products]);

  const togglePreset = (id: string) => setSelectedPresets((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });

  const toggle = (id: number) =>
    setOpened((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const branch = (node: CategoryNode, depth: number) => (
    <div key={node.id}>
      <button
        className={category === node.id ? "active" : ""}
        style={{ paddingLeft: 10 + depth * 14 }}
        aria-expanded={node.children.length > 0 ? opened.has(node.id) : undefined}
        onClick={() => {
          setQuery("");
          setCategory(node.id);
          if (node.children.length > 0) toggle(node.id);
        }}
      >
        <span>
          {node.children.length > 0 && (
            <i className={opened.has(node.id) ? "twist open" : "twist"} aria-hidden="true">›</i>
          )}
          <CategoryIcon name={node.icon} />{node.name}
        </span>
        <small>{node.count}</small>
      </button>
      {opened.has(node.id) && node.children.map((child) => branch(child, depth + 1))}
    </div>
  );

  const cartCount = Object.values(cart).reduce((sum, value) => sum + value, 0);

  const addToCart = (product: Product) =>
    setCart((current) => ({
      ...current,
      [product.id]: Math.min(
        product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20,
        (current[product.id] ?? 0) + 1,
      ),
    }));
  const changeCartQuantity = (product: Product, delta: number) => setCart((current) => {
    const maximum = product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20;
    const nextQuantity = Math.max(0, Math.min(maximum, (current[product.id] || 0) + delta));
    if (nextQuantity === 0) { const next = { ...current }; delete next[product.id]; return next; }
    return { ...current, [product.id]: nextQuantity };
  });

  const toggleFavorite = (id: string) =>
    setFavorites((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      window.localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
      return next;
    });

  return (
    <main className="storefront">
      {/* Поиска в шапке здесь нет: строка под ней — то, вокруг чего собрана
          страница, а два поля на одном экране заставляют гадать. */}
      <StoreHeader
        cartCount={cartCount}
        favoritesCount={favorites.size}
        query={query}
        onQueryChange={setQuery}
        onCartClick={() => setCartOpen(true)}
        homeNavigation
        catalogMenuItems={headerMenus.catalog}
        plantMenuItems={headerMenus.plants}
        onHomeCategoryPick={(id) => { setQuery(""); setCategory(id); requestAnimationFrame(() => document.getElementById("catalog")?.scrollIntoView({ behavior:"smooth" })); }}
      />

      <section className="home-hero" aria-labelledby="home-title">
        <div className="home-hero-copy">
          <h1 id="home-title">Растения,<br />с которыми<br /><em>хорошо</em><i>.</i></h1>
          <p>Живые растения для дома и офиса.<br />Выбираем лучшее и доставляем по всей России.</p>
          <div className="home-hero-actions">
            <a href="#catalog">Выбрать своё растение <span>→</span></a>
            <button type="button" aria-label="Видео о Фикусин"><b>▶</b> Видео о Фикусин</button>
          </div>
          <div className="home-team"><img src="/assets/redesign/team-avatars.webp" alt="Команда Фикусин" /><span>За вашими растениями<br />ухаживает <b>команда любителей</b></span></div>
        </div>
        <div className="home-hero-visual">
          <img src="/assets/redesign/home-hero-4k.webp" alt="Алоказия в керамическом кашпо" />
          <span className="home-stamp" aria-label="Живые растения для живых людей"><svg viewBox="0 0 132 132" aria-hidden="true"><defs><path id="stamp-top" d="M25 64 A41 41 0 0 1 107 64" /><path id="stamp-bottom" d="M25 74 A41 41 0 0 0 107 74" /></defs><circle cx="66" cy="66" r="58" /><circle cx="66" cy="66" r="52" strokeDasharray="2 4" /><text><textPath href="#stamp-top" startOffset="50%" textAnchor="middle">ЖИВЫЕ РАСТЕНИЯ</textPath></text><text><textPath href="#stamp-bottom" startOffset="50%" textAnchor="middle">ДЛЯ ЖИВЫХ ЛЮДЕЙ</textPath></text><path className="stamp-plant" d="M66 82V55m0 10c-12 0-16-8-16-14 9 0 16 4 16 14Zm0 7c12 0 16-8 16-14-9 0-16 4-16 14ZM55 85h22" /></svg></span>
          <span className="home-note delivery"><svg className="note-icon" viewBox="0 0 48 58" aria-hidden="true"><path d="M9 24 24 16l15 8v25L24 56 9 49V24Zm15-8v40m-15-32 15 8 15-8M24 16V3m0 9c-8 0-11-5-11-10 7 0 11 3 11 10Zm0-2c7 0 10-5 10-9-6 0-10 3-10 9Z" /></svg><b>Доставка<br />по всей России</b></span>
          <span className="home-note packing"><svg className="note-icon" viewBox="0 0 48 58" aria-hidden="true"><path d="M9 24 24 16l15 8v25L24 56 9 49V24Zm15-8v40m-15-32 15 8 15-8M24 16V3m0 9c-8 0-11-5-11-10 7 0 11 3 11 10Zm0-2c7 0 10-5 10-9-6 0-10 3-10 9Z" /></svg><b>Аккуратно упакуем<br />и довезём в лучшем виде</b><i>→</i></span>
        </div>
      </section>

      <CollectionStrip products={products} active={selectedPresets} onPick={togglePreset} />

      <section className="storefront-shell" id="catalog">
        <aside className="storefront-side">
          <nav className="storefront-tree">
            <button
              className={category == null ? "active" : ""}
              onClick={() => {
                setQuery("");
                setCategory(null);
              }}
            >
              <span>Весь каталог</span>
              <small>{products.length}</small>
            </button>
            {tree.map((root) => branch(root, 0))}
          </nav>

          {/* На телефоне пять выпадающих списков занимали экран целиком, и
              покупатель прокручивал страницу фильтров, прежде чем увидеть
              первое растение. Дерево разделов остаётся на виду — это
              навигация, а подбор по характеристикам сворачивается. */}
          <details className="storefront-filters" open={filtersOpen} onToggle={(event) => setFiltersOpen(event.currentTarget.open)}>
            <summary>Подбор по характеристикам{activeFilterCount > 0 && <b>{activeFilterCount}</b>}</summary>
            <label className="storefront-check">
              <input
                type="checkbox"
                checked={inStockOnly}
                onChange={(event) => setInStockOnly(event.target.checked)}
              />
              Только в наличии
            </label>
            {facets.length > 0 && <div className="storefront-attribute-filters"><p className="storefront-side-title">Характеристики</p>{facets.map(([code, facet]) => <label key={code}>{facet.name}{facet.unit ? `, ${facet.unit}` : ""}<select value={attributeFilters[code] || ""} onChange={(event) => setAttributeFilters((current) => ({ ...current, [code]: event.target.value }))}><option value="">Любое значение</option>{[...facet.values].sort((a,b) => a.localeCompare(b,"ru",{numeric:true})).map((value) => <option key={value} value={value}>{attributeLabel(value)}</option>)}</select></label>)}</div>}
          </details>
        </aside>

        <div className="storefront-main">
          <div className="storefront-head">
            <div>
              {/* Единственный h1 страницы. Без него поисковик и скринридер
                  видели первым заголовком название случайного товара. */}
              <h2>{searching ? "Результаты поиска" : categoryName || "Каталог"}</h2>
              <p>
                {searching ? `Нашли ${visible.length}` : `${visible.length} товаров`}
                {searching && <span> по запросу «{query.trim()}»</span>}
              </p>
            </div>
          </div>

          <div className="home-catalog-toolbar">
            <button type="button" className={filtersOpen ? "home-filter-button active" : "home-filter-button"} aria-expanded={filtersOpen} onClick={() => setFiltersOpen((value) => !value)}><span className="filter-sliders" aria-hidden="true">☷</span><span>Фильтры</span><i>⌄</i>{activeFilterCount > 0 && <b>{activeFilterCount}</b>}</button>
            <div className="home-filter-group">{catalogFacets.map(([code, facet]) => <CatalogDropdown key={code} label={facet.name} value={attributeFilters[code] || ""} onChange={(value) => setAttributeFilters((current) => ({ ...current, [code]: value }))} options={[...facet.values].sort((a,b) => a.localeCompare(b,"ru",{numeric:true})).map((value) => ({ value, label:`${attributeLabel(value)}${facet.unit ? ` ${facet.unit}` : ""}` }))} />)}</div>
            <CatalogDropdown className="home-sort" label="По популярности" value={sort} onChange={setSort} options={[{value:"popular",label:"По популярности"},{value:"cheap",label:"Сначала дешевле"},{value:"expensive",label:"Сначала дороже"}]} />
            <div className="catalog-view-toggle" aria-label="Вид каталога"><button type="button" className={viewMode === "grid" ? "active" : ""} onClick={() => setViewMode("grid")} aria-label="Плитка">⊞</button><button type="button" className={viewMode === "list" ? "active" : ""} onClick={() => setViewMode("list")} aria-label="Список">☰</button></div>
          </div>
          {filtersOpen && <div className="home-filter-panel"><strong>Все фильтры</strong><label className="storefront-check"><input type="checkbox" checked={inStockOnly} onChange={(event) => setInStockOnly(event.target.checked)} />Только в наличии</label><button type="button" onClick={() => { setInStockOnly(false); setAttributeFilters({}); setSelectedPresets(new Set()); }}>Сбросить фильтры</button></div>}

          {loading && <p className="storefront-empty">Загружаем каталог…</p>}
          {!loading && error && <p className="storefront-empty">{error}</p>}

          {!loading && !error && visible.length === 0 && (
            <div className="storefront-empty">
              <strong>{searching ? "Ничего не нашли" : "Здесь пока пусто"}</strong>
              <p>
                {searching
                  ? "Проверьте написание или поищите короче — например, «фикус» вместо «фикус бенджамина большой»."
                  : "Выберите другую ветку каталога или снимите подборку."}
              </p>
              <button
                onClick={() => {
                  setQuery("");
                  setSelectedPresets(new Set());
                  setCategory(null);
                  setInStockOnly(false);
                  setAttributeFilters({});
                }}
              >
                Показать весь каталог
              </button>
            </div>
          )}

          <div className={`storefront-grid ${viewMode === "list" ? "list-view" : ""}`}>
            {visible.slice(0, visibleLimit).map((product) => {
              const inCart = cart[product.id] ?? 0;
              const preorder = (product.stock ?? 0) <= 0;
              return (
                <article key={product.id} className={preorder ? "storefront-card preorder" : "storefront-card"}>
                  <button
                    className={favorites.has(product.id) ? "storefront-fav active" : "storefront-fav"}
                    onClick={() => toggleFavorite(product.id)}
                    aria-label="В избранное"
                  >
                    ♥
                  </button>
                  <a className="storefront-image" href={`/product/${product.id}`}>
                    <img src={product.image} alt={product.name} loading="lazy" />
                  </a>
                  <a className="storefront-name" href={`/product/${product.id}`}>{product.name}</a>
                  {product.filterAttributes?.some((attribute) => attribute.badge) && <div className="storefront-attribute-badges">{product.filterAttributes.filter((attribute) => attribute.badge).slice(0,2).map((attribute) => <span key={attribute.code}>{attribute.name}: {attributeValue(attribute.value, attribute.unit)}</span>)}</div>}
                  {product.latin && <p className="storefront-latin">{product.latin}</p>}
                  {product.reviewsCount > 0 && <p className="storefront-rating"><span>★</span> {product.rating.toFixed(1)} <small>({product.reviewsCount})</small></p>}
                  <div className="storefront-buy">
                    <span className="storefront-price"><strong>{money(product.price)}</strong>{preorder && <em>Под заказ</em>}</span>
                    {inCart > 0 ? <div className="storefront-quantity"><button type="button" onClick={() => changeCartQuantity(product,-1)} aria-label="Уменьшить количество">−</button><button type="button" className="quantity-value" onClick={() => setCartOpen(true)} aria-label={`В корзине · ${inCart}`}>{inCart}</button><button type="button" onClick={() => changeCartQuantity(product,1)} aria-label="Увеличить количество">+</button></div> : <button
                      onClick={() => addToCart(product)}
                      aria-label="В корзину"
                      title="Добавить в корзину"
                    >
                      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4.5 7.5h2l1.4 9.2h9.8l1.8-6.5H7.1M9.5 20a1 1 0 1 0 0-2 1 1 0 0 0 0 2Zm7 0a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z" /></svg>
                      <span className="cart-button-label">В корзину</span>
                    </button>}
                  </div>
                </article>
              );
            })}
          </div>
          {visible.length > visibleLimit && <button className="storefront-more" type="button" onClick={() => setVisibleLimit((value) => value + 12)}>Показать ещё растения <span>⌄</span></button>}
        </div>
      </section>
      <section className="home-service" id="care" aria-label="Помощь с выбором и доставкой">
        <div className="home-service-choice"><h2>Не знаете,<br />что выбрать?</h2><p>Напишите нам в чат — подскажем лучшее растение<br />для вашего интерьера и уровня освещения.</p><a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Написать в чат <span>◯</span></a><div className="home-service-team"><img src="/assets/redesign/team-avatars.webp" alt="" /><span>Команда Фикусин<br />всегда на связи</span></div></div>
        <div className="home-service-card delivery"><b>Бережно доставим<br />по всей России</b><span>Надёжная упаковка<br />и бережная доставка</span><i aria-hidden="true">→</i></div>
        <div className="home-service-card care"><b>Поможем с уходом<br />и пересадкой</b><span>Ответим на вопросы<br />и подскажем</span></div>
      </section>
      <CheckoutHost
        cart={cart}
        products={products}
        cartOpen={cartOpen}
        onCartOpenChange={setCartOpen}
        onCartChange={setCart}
      />
    </main>
  );
}
