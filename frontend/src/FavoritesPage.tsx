import { useEffect, useMemo, useState } from "react";
import { StoreHeader, STORAGE_EVENT } from "./StoreHeader";
import { useSharedCart } from "./lib/cart";

type Product = { id: string; name: string; latin: string; price: number; image: string; size: string; light: string; stock: number };
const money = (value: number) => new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);

export default function FavoritesPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [query, setQuery] = useState("");
  const [favorites, setFavorites] = useState<Set<string>>(() => { try { return new Set(JSON.parse(localStorage.getItem("ficusin-favorites") || "[]") as string[]); } catch { return new Set(); } });
  const [cart, setCart] = useSharedCart();
  useEffect(() => { fetch("/api/v1/catalog", { cache: "no-store" }).then((response) => response.json()).then((data: { products?: Product[] }) => setProducts(data.products || [])); }, []);
  const items = useMemo(() => products.filter((product) => favorites.has(product.id) && `${product.name} ${product.latin}`.toLowerCase().includes(query.toLowerCase())), [products, favorites, query]);
  const remove = (id: string) => { const next = new Set(favorites); next.delete(id); setFavorites(next); localStorage.setItem("ficusin-favorites", JSON.stringify([...next])); window.dispatchEvent(new Event(STORAGE_EVENT)); };
  const add = (product: Product) => setCart((current) => ({ ...current, [product.id]: Math.min(product.stock, (current[product.id] || 0) + 1) }));
  return <main><StoreHeader query={query} onQueryChange={setQuery} favoritesCount={favorites.size} cartCount={Object.values(cart).reduce((sum, value) => sum + value, 0)} />
    <section className="favorites-page"><div className="favorites-hero"><div><p className="eyebrow">Сохранённые товары</p><h1>Ваши любимые<br/>растения</h1><p>Сохраняйте находки и возвращайтесь к ним, когда будете готовы выбрать.</p></div></div>
      <div className="product-grid">{items.map((product) => <article className="product-card" key={product.id}>
        <button className="favorite-button active" onClick={() => remove(product.id)} aria-label="Убрать из избранного">♥</button>
        <a className="product-image" href={`/product/${product.id}`}><img src={product.image} alt={product.name} /></a>
        <div className="product-info"><p className="latin">{product.latin}</p><h3><a href={`/product/${product.id}`}>{product.name}</a></h3>
          <div className="product-meta"><span>{product.light}</span><span>{product.size}</span></div>
          <div className="product-bottom"><strong>{money(product.price)}</strong><button className={cart[product.id] ? "in-cart" : undefined} onClick={() => add(product)}>{cart[product.id] ? `Добавить ещё · ${cart[product.id]} шт.` : "В корзину"}</button></div>
        </div></article>)}</div>
      {!items.length && <div className="empty-state"><strong>{query ? "Ничего не найдено" : "В избранном пока пусто"}</strong><span>Добавляйте товары красным сердечком в каталоге.</span><a href="/#catalog">Перейти в каталог</a></div>}
    </section>
  </main>;
}
