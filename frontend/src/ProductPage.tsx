import { useEffect, useMemo, useState } from "react";
import { StoreHeader } from "./StoreHeader";

type CatalogProduct = { id: string; name: string; latin: string; price: number; image: string; size: string; stock: number };
type Variant = { id: number; sku: string; label: string; price: number; stock: number; heightCm?: number; potDiameterCm?: number; wholesaleMinQty: number };
type ProductDetail = {
  id: string; name: string; latin: string; shortDescription: string; description: string;
  careInstructions: string; images: string[]; variants: Variant[]; recommendations: CatalogProduct[];
  catalogSection: string; plantKind?: string; lightLevel?: string; watering?: string;
  heightClass?: string; careLevel?: string; placement?: string; petSafety?: string; growthHabit?: string;
};

const money = (value: number) => new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);

export default function ProductPage({ slug }: { slug: string }) {
  const [product, setProduct] = useState<ProductDetail | null>(null);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [activeImage, setActiveImage] = useState(0);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [quantity, setQuantity] = useState(1);
  const [favorites, setFavorites] = useState<Set<string>>(() => {
    try { return new Set(JSON.parse(localStorage.getItem("ficusin-favorites") || "[]") as string[]); }
    catch { return new Set(); }
  });
  const [cart, setCart] = useState<Record<string, number>>(() => {
    try { return JSON.parse(localStorage.getItem("ficusin-cart") || "{}") as Record<string, number>; }
    catch { return {}; }
  });
  const cartCount = Object.values(cart).reduce((sum, value) => sum + value, 0);

  useEffect(() => {
    fetch(`/api/v1/products/${encodeURIComponent(slug)}`, { cache: "no-store" })
      .then(async (response) => { const body = await response.json() as { product?: ProductDetail; error?: string }; if (!response.ok || !body.product) throw new Error(body.error || "Товар не найден"); return body.product; })
      .then((item) => { setProduct(item); setSelectedID(item.variants[0]?.id ?? null); document.title = `${item.name} — Фикусин`; })
      .catch((reason) => setError(reason instanceof Error ? reason.message : "Не удалось загрузить товар"));
  }, [slug]);

  const variant = useMemo(() => product?.variants.find((item) => item.id === selectedID) || product?.variants[0], [product, selectedID]);
  const toggleFavorite = () => {
    if (!product) return;
    setFavorites((current) => {
      const next = new Set(current);
      if (next.has(product.id)) next.delete(product.id); else next.add(product.id);
      localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
      return next;
    });
  };

  const addToCart = () => {
    if (!product || !variant || variant.stock <= 0) return;
    let stored: Record<string, number> = {};
    try { stored = JSON.parse(localStorage.getItem("ficusin-cart") || "{}"); } catch { stored = {}; }
    stored[product.id] = Math.min(variant.stock, quantity);
    localStorage.setItem("ficusin-cart", JSON.stringify(stored));
    setCart({ ...stored });
    setNotice(cart[product.id] ? "Количество обновлено" : "Товар добавлен в корзину"); window.setTimeout(() => setNotice(""), 1800);
  };

  if (error) return <main className="product-page"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} /><section className="pdp-error"><h1>{error}</h1><a href="/#catalog">Вернуться в каталог</a></section></main>;
  if (!product) return <main className="product-page"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} /><section className="pdp-error"><p>Загружаем карточку товара…</p></section></main>;

  const warningBadges = [
    product.petSafety === "toxic" ? "Ядовито для животных" : product.petSafety === "safe" ? "Безопасно для животных" : "",
    product.lightLevel === "sunny" ? "Нужно яркое освещение" : "",
    product.watering === "frequent" ? "Нужен частый полив" : "",
  ].filter(Boolean).slice(0, 3);

  return <main className="product-page">
    <StoreHeader cartCount={cartCount} favoritesCount={favorites.size} />
    <nav className="breadcrumbs"><a href="/">Главная</a><span>→</span><a href="/#catalog">Каталог</a><span>→</span><b>{product.name}</b></nav>
    <section className="pdp-main">
      <div className="pdp-gallery"><div className="pdp-thumbs">{product.images.map((image, index) => <button className={activeImage === index ? "active" : ""} onClick={() => setActiveImage(index)} key={`${image}-${index}`}><img src={image} alt="" /></button>)}</div><div className="pdp-image"><img src={product.images[activeImage] || product.images[0]} alt={product.name} /></div></div>
      <div className="pdp-summary"><p className="latin">{product.latin}</p><h1>{product.name}</h1><p className="pdp-lead">{product.shortDescription || product.description || "Живое растение из каталога Фикусин. Перед отправкой проверим состояние и бережно упакуем."}</p>
        {product.variants.length > 1 && <div className="variant-picker"><span>Выберите размер</span>{product.variants.map((item) => <button className={item.id === variant?.id ? "active" : ""} onClick={() => { setSelectedID(item.id); setQuantity(1); }} key={item.id}><strong>{item.label}</strong><small>{money(item.price)}</small></button>)}</div>}
        {variant && <div className="pdp-specs">{variant.heightCm && <div><span>Высота</span><strong>{variant.heightCm} см</strong></div>}{variant.potDiameterCm && <div><span>Горшок</span><strong>Ø {variant.potDiameterCm} см</strong></div>}<div><span>Артикул</span><strong>{variant.sku}</strong></div><div><span>Наличие</span><strong>{variant.stock > 0 ? `${variant.stock} шт.` : "Нет"}</strong></div></div>}
        {warningBadges.length > 0 && <div className="pdp-warnings">{warningBadges.map((badge) => <span key={badge}>{badge}</span>)}</div>}
        <div className="pdp-buy"><strong>{variant ? money(variant.price) : "Цена уточняется"}</strong><div className="pdp-quantity" aria-label="Количество"><button type="button" onClick={() => setQuantity((value) => Math.max(1, value - 1))} disabled={quantity <= 1}>−</button><output>{quantity}</output><button type="button" onClick={() => setQuantity((value) => Math.min(Math.min(variant?.stock || 1, 20), value + 1))} disabled={!variant || quantity >= Math.min(variant.stock, 20)}>+</button></div><button className={favorites.has(product.id) ? "pdp-favorite active" : "pdp-favorite"} onClick={toggleFavorite} aria-label="Добавить в избранное">{favorites.has(product.id) ? "♥" : "♡"}</button><button className={cart[product.id] ? "in-cart" : undefined} onClick={addToCart} disabled={!variant || variant.stock <= 0}>{!variant?.stock ? "Нет в наличии" : cart[product.id] ? "Обновить корзину" : "Добавить в корзину"}</button></div>
        <div className="pdp-benefits"><p>✓ Проверим растение перед отправкой</p><p>✓ Упакуем с учётом погоды</p><p>✓ Доставка по Рязани и России</p></div>
      </div>
    </section>
    <section className="pdp-content"><article><p className="eyebrow">О растении</p><h2>Описание</h2><p>{product.description || "Описание готовится. Подробности можно уточнить у консультанта."}</p></article><article><p className="eyebrow">После покупки</p><h2>Уход</h2><p>{product.careInstructions || "Мы приложим рекомендации по поливу, освещению и пересадке к вашему заказу."}</p></article></section>
    {product.recommendations.length > 0 && <section className="pdp-related"><div><p className="eyebrow">Вам может понравиться</p><h2>Похожие растения</h2></div><div className="product-grid">{product.recommendations.map((item) => <a className="product-card related-card" href={`/product/${item.id}`} key={item.id}><div className="product-image"><img src={item.image} alt={item.name} /></div><div className="product-info"><p className="latin">{item.latin}</p><h3>{item.name}</h3><strong>{money(item.price)}</strong></div></a>)}</div></section>}
    <footer className="pdp-footer"><a className="brand" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></a><p>Рязань, Новосёлов, 40А · +7 915 615-11-00 · ежедневно 08:00–20:00</p></footer>
    {notice && <div className="toast">{notice}</div>}
  </main>;
}
