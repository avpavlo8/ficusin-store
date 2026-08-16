import { useEffect, useMemo, useState } from "react";
import CheckoutHost from "./CheckoutHost";
import { StoreHeader } from "./StoreHeader";
import { CollectionStrip, presets } from "./Collections";
import { searchProducts } from "./lib/search";
import { CatalogSearch } from "./CatalogSearch";
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
  const [category, setCategory] = useState<number | null>(null);
  // На широком экране подбор раскрыт сразу, на телефоне — по нажатию.
  const [filtersOpen, setFiltersOpen] = useState(() => window.matchMedia("(min-width: 901px)").matches);
  const [opened, setOpened] = useState<Set<number>>(new Set());
  const [selectedPresets, setSelectedPresets] = useState<Set<string>>(new Set());
  const [inStockOnly, setInStockOnly] = useState(false);
  const [attributeFilters, setAttributeFilters] = useState<Record<string, string>>({});
  const [sort, setSort] = useState("popular");

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
    for (const [code, selected] of Object.entries(attributeFilters)) if (selected) list = list.filter((product) => product.filterAttributes?.some((attribute) => attribute.code === code && (Array.isArray(attribute.value) ? attribute.value.map(String).includes(selected) : String(attribute.value) === selected)));
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
        showSearch={false}
        onCartClick={() => setCartOpen(true)}
      />

      <div className="storefront-search-bar">
        <CatalogSearch value={query} onChange={setQuery} inlineResults className="storefront-search" placeholder="Поиск: монстера, фикус, кашпо 15 см" />
      </div>

      <section className="storefront-shell" id="catalog">
        <aside className="storefront-side">
          <p className="storefront-side-title">Каталог</p>
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
          <CollectionStrip products={products} active={selectedPresets} onPick={togglePreset} />

          <div className="storefront-head">
            <div>
              {/* Единственный h1 страницы. Без него поисковик и скринридер
                  видели первым заголовком название случайного товара. */}
              <h1>{searching ? "Результаты поиска" : categoryName || "Комнатные растения"}</h1>
              <p>
                {searching ? `Нашли ${visible.length}` : `${visible.length} товаров`}
                {searching && <span> по запросу «{query.trim()}»</span>}
              </p>
            </div>
            <select value={sort} onChange={(event) => setSort(event.target.value)} aria-label="Сортировка">
              <option value="popular">сначала популярные</option>
              <option value="cheap">сначала дешёвые</option>
              <option value="expensive">сначала дорогие</option>
            </select>
          </div>

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

          <div className="storefront-grid">
            {visible.map((product) => {
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
                  {preorder && <p className="storefront-preorder">Под заказ · срок уточнит менеджер</p>}
                  <div className="storefront-buy">
                    <strong>{money(product.price)}</strong>
                    <button
                      className={inCart ? "in-cart" : ""}
                      onClick={() => (inCart ? setCartOpen(true) : addToCart(product))}
                    >
                      {inCart ? `В корзине · ${inCart}` : preorder ? "Под заказ" : "В корзину"}
                    </button>
                  </div>
                </article>
              );
            })}
          </div>
        </div>
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
