import "./styles/customer-pages.css";
import { usePageTitle } from "./lib/usePageTitle";
import { useEffect, useMemo, useState } from "react";
import { StoreHeader } from "./StoreHeader";
import { useSharedCart } from "./lib/cart";
import { sharedAPIJSON } from "./lib/api";
import { useFavorites } from "./lib/favorites";
import { counted } from "./lib/storefront-format";
import { CatalogProductCard, type CatalogCardProduct } from "./CatalogProductCard";

type Product = CatalogCardProduct;

export default function FavoritesPage() {
  usePageTitle("Избранное");
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
  const add = (product: Product) => setCart((current) => ({ ...current, [product.sku]: Math.min(product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20, (current[product.sku] || 0) + 1) }));
  return <main className="storefront"><StoreHeader favoritesCount={status === "ready" ? items.length : undefined} cartCount={Object.values(cart).reduce((sum, value) => sum + value, 0)} />
    <section className="favorites-page"><header className="favorites-heading"><h1>Избранное</h1>{status === "ready" && <span>{counted(items.length, ["товар", "товара", "товаров"])}</span>}</header>
      <p className="customer-intro">Сохранённые растения на этом устройстве. Возвращайтесь к ним, когда будете готовы выбрать.</p>
      {status === "loading" && <p role="status">Загружаем избранное…</p>}
      {status === "error" && <div className="empty-state" role="alert"><strong>Не удалось загрузить избранное</strong><p>Сохранённые товары на месте. Попробуйте загрузить их ещё раз.</p><button type="button" onClick={load}>Повторить</button></div>}
      {status === "ready" && unavailable.length > 0 && <p className="favorites-unavailable" role="status">{counted(unavailable.length, ["сохранённый товар недоступен", "сохранённых товара недоступны", "сохранённых товаров недоступны"])}. <button type="button" aria-label="Убрать недоступные из избранного" onClick={() => setFavorites((current) => new Set([...current].filter((id) => !unavailable.includes(id))))}>Убрать из списка</button></p>}
      <div className="storefront-grid">{items.map(product => <CatalogProductCard key={product.id} product={product} favorite inCart={cart[product.sku] ?? 0} onFavorite={() => remove(product.id)} onAdd={() => add(product)} onDecrease={() => setCart(current => {
        const next = { ...current };
        if ((next[product.sku] || 0) <= 1) delete next[product.sku]; else next[product.sku] -= 1;
        return next;
      })} />)}</div>
      {status === "ready" && !favorites.size && <div className="empty-state"><strong>В избранном пока пусто</strong><span>Добавляйте товары сердечком в каталоге.</span><a href="/#catalog">Перейти в каталог</a></div>}
    </section>
  </main>;
}
