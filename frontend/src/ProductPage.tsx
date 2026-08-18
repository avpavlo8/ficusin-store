import { useEffect, useMemo, useState } from "react";
import { StoreHeader, STORAGE_EVENT } from "./StoreHeader";
import { ProductGallery } from "./product/ProductGallery";
import { ProductPurchasePanel } from "./product/ProductPurchasePanel";
import { PlantPassport } from "./product/PlantPassport";
import { ProductReviews, ReviewComposer } from "./product/ProductReviews";
import type { ProductDetail } from "./product/types";
import { attributeValue, money } from "./product/types";

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
      .then((item) => { const normalized = { ...item, passport: item.passport || {}, importantWarnings: item.importantWarnings || [], attributes: item.attributes || [], reviews: item.reviews || [], rating: Number(item.rating) || 0, reviewsCount: Number(item.reviewsCount) || 0 }; setProduct(normalized); setSelectedID(normalized.variants[0]?.id ?? null); document.title = `${normalized.name} — Фикусин`; })
      .catch((reason) => setError(reason instanceof Error ? reason.message : "Не удалось загрузить товар"));
  }, [slug]);

  const variant = useMemo(() => product?.variants.find((item) => item.id === selectedID) || product?.variants[0], [product, selectedID]);
  const toggleFavorite = () => {
    if (!product) return;
    setFavorites((current) => {
      const next = new Set(current);
      if (next.has(product.id)) next.delete(product.id); else next.add(product.id);
      localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
      window.dispatchEvent(new Event(STORAGE_EVENT));
      return next;
    });
  };

  const addToCart = () => {
    if (!product || !variant || variant.stock <= 0) return;
    let stored: Record<string, number> = {};
    try { stored = JSON.parse(localStorage.getItem("ficusin-cart") || "{}"); } catch { stored = {}; }
    stored[product.id] = Math.min(variant.stock, quantity);
    localStorage.setItem("ficusin-cart", JSON.stringify(stored));
    window.dispatchEvent(new Event(STORAGE_EVENT));
    setCart({ ...stored });
    setNotice(cart[product.id] ? "Количество обновлено" : "Товар добавлен в корзину"); window.setTimeout(() => setNotice(""), 1800);
  };

  if (error) return <main className="product-page"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} /><section className="pdp-error"><h1>{error}</h1><a href="/#catalog">Вернуться в каталог</a></section></main>;
  if (!product) return <main className="product-page"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} /><section className="pdp-error"><p>Загружаем карточку товара…</p></section></main>;

  // Здесь только то, о чём покупателя действительно нужно предупредить.
  // Раньше сюда подставлялись обычные характеристики, и «Освещение:
  // Полутень» выводилось с оранжевым восклицательным знаком, как ошибка.
  // Характеристики целиком показаны ниже, в «Подробно о товаре».
  const warningBadges = (product.importantWarnings?.length ? product.importantWarnings : [
    product.petSafety === "toxic" ? "Ядовито для животных" : "",
    product.lightLevel === "sunny" ? "Нужно яркое освещение" : "",
    product.watering === "frequent" ? "Нужен частый полив" : "",
  ]).filter(Boolean).slice(0, 4);

  return <main className="product-page">
    <StoreHeader cartCount={cartCount} favoritesCount={favorites.size} />
    <nav className="breadcrumbs" aria-label="Хлебные крошки"><a href="/">Главная</a><span>/</span><a href="/#catalog">Каталог</a><span>/</span><b>{product.name}</b></nav>
    <section className="pdp-main">
      <ProductGallery images={product.images} name={product.name} active={activeImage} onSelect={setActiveImage} />
      <ProductPurchasePanel product={product} variant={variant} quantity={quantity} favorite={favorites.has(product.id)} inCart={Boolean(cart[product.id])} warnings={warningBadges} reviewComposer={<ReviewComposer slug={slug} rating={product.rating} count={product.reviewsCount} />} onVariant={(id) => { setSelectedID(id); setQuantity(1); }} onQuantity={setQuantity} onFavorite={toggleFavorite} onBuy={addToCart} />
    </section>
    <nav className="pdp-anchor-nav" aria-label="Разделы товара"><a href="#about">О растении</a><a href="#plant-passport">Паспорт</a><a href="#reviews">Отзывы {product.reviewsCount > 0 && <span>{product.reviewsCount}</span>}</a></nav>
    <section className="pdp-info-grid" aria-label="Информация о растении">
      <section className="pdp-content pdp-section pdp-info-card" id="about"><header className="pdp-section-heading"><h2>Уход за растением</h2></header><div><article><h3>О растении</h3><p>{product.description || "Описание готовится. Подробности можно уточнить у консультанта."}</p></article><article><h3>Базовый уход</h3><p>{product.careInstructions || "Мы приложим рекомендации по поливу, освещению и пересадке к вашему заказу."}</p></article>{product.attributes.length > 0 && <article className="pdp-compact-attributes"><h3>Характеристики</h3><dl>{product.attributes.slice(0,6).map((item) => <div key={item.code}><dt>{item.name}</dt><dd>{attributeValue(item.value, item.unit)}</dd></div>)}</dl></article>}</div></section>
      <PlantPassport name={product.name} passport={product.passport} />
      <ProductReviews reviews={product.reviews} />
    </section>
    {product.recommendations.length > 0 && <section className="pdp-related"><header><div><p className="eyebrow">Вам может понравиться</p><h2>Похожие растения</h2></div><a href="/#catalog">Смотреть все <span>→</span></a></header><div className="pdp-related-track">{product.recommendations.map((item) => <a className="product-card related-card" href={`/product/${item.id}`} key={item.id}><div className="product-image"><img src={item.image} alt={item.name} /></div><div className="product-info"><p className="latin">{item.latin}</p><h3>{item.name}</h3><strong>{money(item.price)}</strong><span className="related-arrow" aria-hidden="true">→</span></div></a>)}</div></section>}
    <footer className="pdp-footer"><a className="brand" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></a><p>Рязань, Новосёлов, 40А · +7 915 615-11-00 · ежедневно 08:00–20:00</p></footer>
    {notice && <div className="toast">{notice}</div>}
  </main>;
}
