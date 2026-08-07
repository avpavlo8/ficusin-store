import { useEffect, useMemo, useRef, useState } from "react";
import { StoreHeader } from "./StoreHeader";
import { CollectionStrip, presets } from "./Collections";
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
  placement?: string;
  watering?: string;
  categoryId?: number;
};

type Category = { id: number; parentId: number | null; name: string; slug: string; sortOrder: number };
type Cart = Record<string, number>;

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(value);

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
  const [suggestOpen, setSuggestOpen] = useState(false);
  const [category, setCategory] = useState<number | null>(null);
  const [opened, setOpened] = useState<number | null>(null);
  const [preset, setPreset] = useState("");
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
    fetch("/api/v1/categories", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { categories?: Category[] }) => setCategories(data.categories ?? []))
      .catch(() => setCategories([]));
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

  // Сколько товаров лежит в ветке дерева вместе со всеми её потомками.
  const countIn = useMemo(() => {
    const children = new Map<number, number[]>();
    categories.forEach((item) => {
      if (item.parentId == null) return;
      children.set(item.parentId, [...(children.get(item.parentId) ?? []), item.id]);
    });
    const direct = new Map<number, number>();
    products.forEach((item) => {
      if (item.categoryId == null) return;
      direct.set(item.categoryId, (direct.get(item.categoryId) ?? 0) + 1);
    });
    const cache = new Map<number, number>();
    const walk = (id: number): number => {
      const seen = cache.get(id);
      if (seen != null) return seen;
      const total =
        (direct.get(id) ?? 0) +
        (children.get(id) ?? []).reduce((sum, child) => sum + walk(child), 0);
      cache.set(id, total);
      return total;
    };
    return walk;
  }, [categories, products]);

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

  const roots = useMemo(
    () =>
      categories
        .filter((item) => item.parentId == null && countIn(item.id) > 0)
        .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name)),
    [categories, countIn],
  );

  const childrenOf = (parent: number) =>
    categories
      .filter((item) => item.parentId === parent && countIn(item.id) > 0)
      .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name));

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
    if (preset) {
      const rule = presets.find((item) => item.id === preset);
      if (rule) list = list.filter(rule.match);
    }
    if (inStockOnly) list = list.filter((item) => (item.stock ?? 0) > 0);
    if (sort === "cheap") list = [...list].sort((a, b) => a.price - b.price);
    if (sort === "expensive") list = [...list].sort((a, b) => b.price - a.price);
    return list;
  }, [found, searching, category, inBranch, preset, inStockOnly, sort]);

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

  return (
    <main className="storefront">
      {/* Поиска в шапке здесь нет: строка под ней — то, вокруг чего собрана
          страница, а два поля на одном экране заставляют гадать. */}
      <StoreHeader
        cartCount={cartCount}
        favoritesCount={favorites.size}
        showSearch={false}
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
                  <span>{hint.latin}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      <section className="storefront-shell">
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
            {roots.map((root) => (
              <div key={root.id}>
                <button
                  className={category === root.id ? "active" : ""}
                  aria-expanded={opened === root.id}
                  onClick={() => {
                    setQuery("");
                    setCategory(root.id);
                    setOpened(opened === root.id ? null : root.id);
                  }}
                >
                  <span>
                    {childrenOf(root.id).length > 0 && (
                      <i className={opened === root.id ? "twist open" : "twist"} aria-hidden="true">›</i>
                    )}
                    {root.name}
                  </span>
                  <small>{countIn(root.id)}</small>
                </button>
                {opened === root.id &&
                  childrenOf(root.id).map((child) => (
                    <button
                      key={child.id}
                      className={category === child.id ? "child active" : "child"}
                      onClick={() => {
                        setQuery("");
                        setCategory(child.id);
                      }}
                    >
                      <span>{child.name}</span>
                      <small>{countIn(child.id)}</small>
                    </button>
                  ))}
              </div>
            ))}
          </nav>

          <label className="storefront-check">
            <input
              type="checkbox"
              checked={inStockOnly}
              onChange={(event) => setInStockOnly(event.target.checked)}
            />
            Только в наличии
          </label>
        </aside>

        <div className="storefront-main">
          <CollectionStrip products={products} active={preset} onPick={setPreset} />

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
              <strong>{searching ? "Ничего не нашли" : "Здесь пока пусто"}</strong>
              <p>
                {searching
                  ? "Проверьте написание или поищите короче — например, «фикус» вместо «фикус бенджамина большой»."
                  : "Выберите другую ветку каталога или снимите подборку."}
              </p>
              <button
                onClick={() => {
                  setQuery("");
                  setPreset("");
                  setCategory(null);
                  setInStockOnly(false);
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
