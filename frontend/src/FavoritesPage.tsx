import { useEffect, useMemo, useState } from "react";
import { StoreHeader, STORAGE_EVENT } from "./StoreHeader";
import { useSharedCart } from "./lib/cart";

type Product = { id: string; sku?: string; name: string; latin: string; price: number; image: string; size: string; light: string; stock: number; rating?: number; reviewsCount?: number };
const money = (value: number) => new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);

export default function FavoritesPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [query, setQuery] = useState("");
  const [favorites, setFavorites] = useState<Set<string>>(() => { try { return new Set(JSON.parse(localStorage.getItem("ficusin-favorites") || "[]") as string[]); } catch { return new Set(); } });
  const [cart, setCart] = useSharedCart();
  useEffect(() => { fetch("/api/v1/catalog").then((response) => response.json()).then((data: { products?: Product[] }) => setProducts(data.products || [])); }, []);
  const items = useMemo(() => products.filter((product) => favorites.has(product.id) && `${product.name} ${product.latin}`.toLowerCase().includes(query.toLowerCase())), [products, favorites, query]);
  const remove = (id: string) => { const next = new Set(favorites); next.delete(id); setFavorites(next); localStorage.setItem("ficusin-favorites", JSON.stringify([...next])); window.dispatchEvent(new Event(STORAGE_EVENT)); };
  const cartKey = (product: Product) => product.sku || product.id;
  const add = (product: Product) => setCart((current) => ({ ...current, [cartKey(product)]: Math.min(Math.max(product.stock, 1), (current[cartKey(product)] || 0) + 1) }));
  return <main><StoreHeader query={query} onQueryChange={setQuery} favoritesCount={favorites.size} cartCount={Object.values(cart).reduce((sum, value) => sum + value, 0)} />
    <section className="favorites-page ui-container"><header className="favorites-heading"><p className="eyebrow">Сохранённые товары</p><h1>Избранное</h1><p>Сохраняйте находки и возвращайтесь к ним, когда будете готовы выбрать.</p></header>
      <div className="storefront-grid">{items.map((product) => <article className="storefront-card ui-card" key={product.id}>
        <button className="storefront-fav active" onClick={() => remove(product.id)} aria-label="Убрать из избранного">♥</button>
        <a className="storefront-image" href={`/product/${product.id}`}><img src={product.image} alt={product.name} loading="lazy" /></a>
        <a className="storefront-name" href={`/product/${product.id}`}>{product.name}</a>
        {product.latin && <p className="storefront-latin">{product.latin}</p>}
        {!!product.reviewsCount && <p className="storefront-rating"><span>★</span> {(product.rating || 0).toFixed(1)} <small>({product.reviewsCount})</small></p>}
        <div className="storefront-buy"><span className="storefront-price"><strong>{money(product.price)}</strong>{product.stock <= 0 && <em>Под заказ</em>}</span><button type="button" onClick={() => add(product)}>{cart[cartKey(product)] ? `В корзине · ${cart[cartKey(product)]}` : "В корзину"}</button></div>
      </article>)}</div>
      {!items.length && <div className="empty-state"><strong>{query ? "Ничего не найдено" : "В избранном пока пусто"}</strong><span>Добавляйте товары красным сердечком в каталоге.</span><a href="/#catalog">Перейти в каталог</a></div>}
    </section>
  </main>;
}
