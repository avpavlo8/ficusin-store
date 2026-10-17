import { useEffect, useMemo, useState } from "react";
import { StoreHeader } from "./StoreHeader";
import { useSharedCart } from "./lib/cart";
import { sharedAPIJSON } from "./lib/api";
import { useFavorites } from "./lib/favorites";
import { catalogSize, counted } from "./lib/storefront-format";
import { money } from "./product/types";

type Product = { id: string; sku: string; name: string; latin: string; price: number; image: string; size: string; stock: number };

export default function FavoritesPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [favorites, setFavorites] = useFavorites();
  const [cart, setCart] = useSharedCart();
  const load = () => {
    setStatus("loading");
    void sharedAPIJSON<{ products?: Product[] }>("/api/v1/catalog", true)
      .then((data) => { setProducts(data.products || []); setStatus("ready"); })
      .catch(() => setStatus("error"));
  };
  useEffect(() => {
    void sharedAPIJSON<{ products?: Product[] }>("/api/v1/catalog")
      .then((data) => { setProducts(data.products || []); setStatus("ready"); })
      .catch(() => setStatus("error"));
  }, []);
  const items = useMemo(() => products.filter((product) => favorites.has(String(product.id))), [products, favorites]);
  const unavailable = [...favorites].filter((id) => !products.some((product) => String(product.id) === id));
  const remove = (id: string) => setFavorites((current) => { const next = new Set(current); next.delete(id); return next; });
  const add = (product: Product) => setCart((current) => ({ ...current, [product.sku]: Math.min(product.stock > 0 ? product.stock : 20, (current[product.sku] || 0) + 1) }));
  return <main><StoreHeader favoritesCount={favorites.size} cartCount={Object.values(cart).reduce((sum, value) => sum + value, 0)} />
    <section className="favorites-page"><div className="favorites-hero"><div><h1>Ваши любимые<br/>растения</h1><p>Сохраняйте находки и возвращайтесь к ним, когда будете готовы выбрать.</p></div></div>
      {status === "loading" && <p role="status">Загружаем избранное…</p>}
      {status === "error" && <div className="empty-state" role="alert"><strong>Не удалось загрузить избранное</strong><p>Сохранённые товары на месте. Попробуйте загрузить их ещё раз.</p><button type="button" onClick={load}>Повторить</button></div>}
      {status === "ready" && unavailable.length > 0 && <div className="favorites-unavailable" role="status"><strong>{counted(unavailable.length, ["сохранённый товар недоступен", "сохранённых товара недоступны", "сохранённых товаров недоступны"])}</strong><p>Эти товары пока не отображаются в каталоге. Мы сохранили их в вашем списке.</p><button type="button" onClick={() => setFavorites((current) => new Set([...current].filter((id) => !unavailable.includes(id))))}>Убрать недоступные из избранного</button></div>}
      <div className="storefront-grid">{items.map((product) => <article className="storefront-card" key={product.id}>
        <button className="storefront-fav active" onClick={() => remove(product.id)} aria-label="Убрать из избранного" aria-pressed="true">♥</button>
        <a className="storefront-image" href={`/product/${product.id}`}><img src={product.image} alt={product.name} loading="lazy" /></a>
        <a className="storefront-name" href={`/product/${product.id}`}>{product.name}</a>
        <p className="storefront-size">{catalogSize(product.size)}</p><p className="storefront-latin">{product.latin}</p>
        <div className="storefront-buy"><span className="storefront-price"><strong>{money(product.price)}</strong><em className={product.stock > 0 ? "available" : ""}>{product.stock > 0 ? "В наличии" : "Под заказ"}</em></span><button onClick={() => add(product)} aria-label={cart[product.sku] ? `Добавить ещё · ${cart[product.sku]} шт.` : product.stock > 0 ? "В корзину" : "Заказать"}>{cart[product.sku] ? `Добавить ещё · ${cart[product.sku]} шт.` : product.stock > 0 ? "В корзину" : "Заказать"}</button></div>
      </article>)}</div>
      {status === "ready" && !favorites.size && <div className="empty-state"><strong>В избранном пока пусто</strong><span>Добавляйте товары сердечком в каталоге.</span><a href="/#catalog">Перейти в каталог</a></div>}
    </section>
  </main>;
}
