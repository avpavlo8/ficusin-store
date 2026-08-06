import { useEffect, useMemo, useRef, useState } from "react";
import { StoreHeader } from "./StoreHeader";
import { searchProducts, suggestions } from "./lib/search";

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
  collections?: string[];
};

type Collection = { slug: string; title: string; note: string; count: number };
type Cart = Record<string, number>;

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(value);

const sections: Array<[string, string]> = [
  ["plants", "Растения"],
  ["pots", "Кашпо и горшки"],
  ["soil", "Грунт"],
  ["fertilizer", "Удобрения"],
  ["accessories", "Аксессуары"],
];

const lightLabels: Record<string, string> = {
  sunny: "Солнечная сторона",
  diffused: "Рассеянный свет",
  low_light: "Тень",
};

const sizeLabels: Record<string, string> = {
  low: "Низкие",
  medium: "Средние",
  high: "Высокие",
};

// The storefront is the home page: products from the first pixel, search in
// the header, collections as tabs above the grid. Searching replaces the
// grid rather than adding results somewhere below — the old page put them
// underneath, where nobody scrolled to find them.
export default function StorefrontPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [collections, setCollections] = useState<Collection[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  // The query comes from the URL before the first render, not from an
  // effect afterwards: setting it later would paint the whole catalogue
  // and immediately throw it away.
  const [query, setQuery] = useState(
    () => new URLSearchParams(window.location.search).get("q") ?? "",
  );
  const [suggestOpen, setSuggestOpen] = useState(false);
  const [section, setSection] = useState("plants");
  const [collection, setCollection] = useState("");
  const [light, setLight] = useState("");
  const [size, setSize] = useState("");
  const [maxPrice, setMaxPrice] = useState(0);
  const [inStockOnly, setInStockOnly] = useState(false);
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
  const searchBox = useRef<HTMLDivElement>(null);

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
    fetch("/api/v1/collections", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { collections?: Collection[] }) => setCollections(data.collections ?? []))
      .catch(() => setCollections([]));
  }, []);

  useEffect(() => {
    if (new URLSearchParams(window.location.search).get("cart") === "1") {
      window.location.assign("/?cart=1");
    }
  }, []);

  useEffect(() => {
    window.localStorage.setItem("ficusin-cart", JSON.stringify(cart));
  }, [cart]);

  useEffect(() => {
    const close = (event: MouseEvent) => {
      if (searchBox.current && !searchBox.current.contains(event.target as Node)) {
        setSuggestOpen(false);
      }
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, []);

  const searching = query.trim().length > 0;

  // Search wins over every filter. Someone standing in "Кашпо" who types
  // "фикус" means the plant, not "no results in this section".
  const found = useMemo(
    () => (searching ? searchProducts(products, query) : products),
    [products, query, searching],
  );

  const visible = useMemo(() => {
    let list = found;
    if (!searching) {
      list = list.filter((item) => item.catalogSection === section);
      if (collection) {
        list = list.filter((item) => (item.collections ?? []).includes(collection));
      }
    }
    if (light) list = list.filter((item) => item.lightLevel === light);
    if (size) list = list.filter((item) => item.heightClass === size);
    if (maxPrice > 0) list = list.filter((item) => item.price <= maxPrice);
    if (inStockOnly) list = list.filter((item) => (item.stock ?? 0) > 0);
    if (sort === "cheap") list = [...list].sort((a, b) => a.price - b.price);
    if (sort === "expensive") list = [...list].sort((a, b) => b.price - a.price);
    return list;
  }, [found, searching, section, collection, light, size, maxPrice, inStockOnly, sort]);

  const hints = useMemo(
    () => (suggestOpen && searching ? suggestions(products, query) : []),
    [products, query, searching, suggestOpen],
  );

  const cartCount = Object.values(cart).reduce((sum, value) => sum + value, 0);

  const addToCart = (product: Product) =>
    setCart((current) => ({
      ...current,
      [product.id]: Math.min(20, (current[product.id] ?? 0) + 1),
    }));

  const toggleFavorite = (id: string) =>
    setFavorites((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      window.localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
      return next;
    });

  const resetFilters = () => {
    setLight("");
    setSize("");
    setMaxPrice(0);
    setInStockOnly(false);
    setCollection("");
  };

  const filtersActive = Boolean(light || size || maxPrice || inStockOnly || collection);

  return (
    <main className="storefront">
      {/* The header search and the bar below it drive the same query: two
          ways in, one result. Leaving the header on its own search would
          give the page two boxes that disagree. */}
      <StoreHeader
        cartCount={cartCount}
        favoritesCount={favorites.size}
        query={query}
        onQueryChange={setQuery}
      />

      <div className="storefront-search-bar">
        <div className="storefront-search" ref={searchBox}>
          <input
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setSuggestOpen(true);
            }}
            onFocus={() => setSuggestOpen(true)}
            placeholder="Поиск: монстера, фикус, кашпо 15 см"
            aria-label="Поиск по каталогу"
            autoComplete="off"
          />
          {query && (
            <button className="storefront-search-clear" onClick={() => setQuery("")} aria-label="Очистить поиск">
              ×
            </button>
          )}
          {hints.length > 0 && (
            <div className="storefront-suggestions" role="listbox">
              {hints.map((hint) => (
                <button
                  key={hint.id}
                  type="button"
                  onClick={() => {
                    setQuery(hint.name);
                    setSuggestOpen(false);
                  }}
                >
                  <b>{hint.name}</b>
                  <span>{hint.latin || sections.find(([id]) => id === hint.catalogSection)?.[1]}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      <section className="storefront-shell">
        <aside className="storefront-side">
          <p className="storefront-side-title">Каталог</p>
          <nav className="storefront-sections">
            {sections.map(([id, title]) => (
              <button
                key={id}
                className={!searching && section === id ? "active" : ""}
                onClick={() => {
                  setQuery("");
                  setSection(id);
                  setCollection("");
                }}
              >
                {title}
              </button>
            ))}
          </nav>

          <p className="storefront-side-title">Фильтры</p>
          <label className="storefront-filter">
            Свет
            <select value={light} onChange={(event) => setLight(event.target.value)}>
              <option value="">любой</option>
              {Object.entries(lightLabels).map(([id, title]) => (
                <option key={id} value={id}>{title}</option>
              ))}
            </select>
          </label>
          <label className="storefront-filter">
            Размер
            <select value={size} onChange={(event) => setSize(event.target.value)}>
              <option value="">любой</option>
              {Object.entries(sizeLabels).map(([id, title]) => (
                <option key={id} value={id}>{title}</option>
              ))}
            </select>
          </label>
          <label className="storefront-filter">
            Цена до, ₽
            <input
              type="number"
              min="0"
              step="100"
              value={maxPrice || ""}
              onChange={(event) => setMaxPrice(Number(event.target.value) || 0)}
              placeholder="без ограничений"
            />
          </label>
          <label className="storefront-check">
            <input
              type="checkbox"
              checked={inStockOnly}
              onChange={(event) => setInStockOnly(event.target.checked)}
            />
            Только в наличии
          </label>
          {filtersActive && (
            <button className="storefront-reset" onClick={resetFilters}>Сбросить фильтры</button>
          )}
        </aside>

        <div className="storefront-main">
          {!searching && collections.length > 0 && (
            <div className="storefront-tabs" role="tablist">
              <button
                className={collection === "" ? "active" : ""}
                onClick={() => setCollection("")}
              >
                Все
              </button>
              {collections.map((item) => (
                <button
                  key={item.slug}
                  className={collection === item.slug ? "active" : ""}
                  onClick={() => setCollection(item.slug)}
                  title={item.note}
                >
                  {item.title}
                  <small>{item.count}</small>
                </button>
              ))}
            </div>
          )}

          <div className="storefront-head">
            <p>
              {searching ? `Нашли ${visible.length}` : `${visible.length} товаров`}
              {searching && <span> по запросу «{query.trim()}»</span>}
            </p>
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
              <strong>{searching ? "Ничего не нашли" : "В этом разделе пока пусто"}</strong>
              <p>
                {searching
                  ? "Проверьте написание или поищите короче — например, «фикус» вместо «фикус бенджамина большой»."
                  : "Загляните в другой раздел или сбросьте фильтры."}
              </p>
              {searching && <button onClick={() => setQuery("")}>Показать весь каталог</button>}
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
                  <p className="storefront-latin">{product.latin || product.size}</p>
                  {preorder && <p className="storefront-preorder">Под заказ · срок уточнит менеджер</p>}
                  <div className="storefront-buy">
                    <strong>{money(product.price)}</strong>
                    <button
                      className={inCart ? "in-cart" : ""}
                      onClick={() => (inCart ? window.location.assign("/?cart=1") : addToCart(product))}
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
    </main>
  );
}
