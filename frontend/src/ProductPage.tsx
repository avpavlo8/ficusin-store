import { useEffect, useMemo, useState } from "react";

type CatalogProduct = { id: string; name: string; latin: string; price: number; image: string; size: string; stock: number };
type Variant = { id: number; sku: string; label: string; price: number; stock: number; heightCm?: number; potDiameterCm?: number; wholesaleMinQty: number };
type ProductDetail = {
  id: string; name: string; latin: string; shortDescription: string; description: string;
  careInstructions: string; images: string[]; variants: Variant[]; recommendations: CatalogProduct[];
};

const money = (value: number) => new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);

export default function ProductPage({ slug }: { slug: string }) {
  const [product, setProduct] = useState<ProductDetail | null>(null);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [activeImage, setActiveImage] = useState(0);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [cartCount, setCartCount] = useState(() => {
    try { return Object.values(JSON.parse(localStorage.getItem("ficusin-cart") || "{}") as Record<string, number>).reduce((sum, value) => sum + value, 0); }
    catch { return 0; }
  });

  useEffect(() => {
    fetch(`/api/v1/products/${encodeURIComponent(slug)}`, { cache: "no-store" })
      .then(async (response) => { const body = await response.json() as { product?: ProductDetail; error?: string }; if (!response.ok || !body.product) throw new Error(body.error || "Товар не найден"); return body.product; })
      .then((item) => { setProduct(item); setSelectedID(item.variants[0]?.id ?? null); document.title = `${item.name} — Фикусин`; })
      .catch((reason) => setError(reason instanceof Error ? reason.message : "Не удалось загрузить товар"));
  }, [slug]);

  const variant = useMemo(() => product?.variants.find((item) => item.id === selectedID) || product?.variants[0], [product, selectedID]);
  const addToCart = () => {
    if (!product || !variant || variant.stock <= 0) return;
    let cart: Record<string, number> = {};
    try { cart = JSON.parse(localStorage.getItem("ficusin-cart") || "{}"); } catch { cart = {}; }
    cart[product.id] = Math.min(variant.stock, (cart[product.id] || 0) + 1);
    localStorage.setItem("ficusin-cart", JSON.stringify(cart));
    setCartCount(Object.values(cart).reduce((sum, value) => sum + value, 0));
    setNotice("Товар добавлен в корзину"); window.setTimeout(() => setNotice(""), 1800);
  };

  if (error) return <main className="product-page"><Header cartCount={cartCount} /><section className="pdp-error"><h1>{error}</h1><a href="/#catalog">Вернуться в каталог</a></section></main>;
  if (!product) return <main className="product-page"><Header cartCount={cartCount} /><section className="pdp-error"><p>Загружаем карточку товара…</p></section></main>;

  return <main className="product-page">
    <Header cartCount={cartCount} />
    <nav className="breadcrumbs"><a href="/">Главная</a><span>→</span><a href="/#catalog">Каталог</a><span>→</span><b>{product.name}</b></nav>
    <section className="pdp-main">
      <div className="pdp-gallery"><div className="pdp-thumbs">{product.images.map((image, index) => <button className={activeImage === index ? "active" : ""} onClick={() => setActiveImage(index)} key={`${image}-${index}`}><img src={image} alt="" /></button>)}</div><div className="pdp-image"><img src={product.images[activeImage] || product.images[0]} alt={product.name} /></div></div>
      <div className="pdp-summary"><p className="latin">{product.latin}</p><h1>{product.name}</h1><p className="pdp-lead">{product.shortDescription || product.description || "Живое растение из каталога Фикусин. Перед отправкой проверим состояние и бережно упакуем."}</p>
        {product.variants.length > 1 && <div className="variant-picker"><span>Выберите размер</span>{product.variants.map((item) => <button className={item.id === variant?.id ? "active" : ""} onClick={() => setSelectedID(item.id)} key={item.id}><strong>{item.label}</strong><small>{money(item.price)}</small></button>)}</div>}
        {variant && <div className="pdp-specs">{variant.heightCm && <div><span>Высота</span><strong>{variant.heightCm} см</strong></div>}{variant.potDiameterCm && <div><span>Горшок</span><strong>Ø {variant.potDiameterCm} см</strong></div>}<div><span>Артикул</span><strong>{variant.sku}</strong></div><div><span>Наличие</span><strong>{variant.stock > 0 ? `${variant.stock} шт.` : "Нет"}</strong></div></div>}
        <div className="pdp-buy"><strong>{variant ? money(variant.price) : "Цена уточняется"}</strong><button onClick={addToCart} disabled={!variant || variant.stock <= 0}>{variant?.stock ? "Добавить в корзину" : "Нет в наличии"}</button></div>
        <div className="pdp-benefits"><p>✓ Проверим растение перед отправкой</p><p>✓ Упакуем с учётом погоды</p><p>✓ Доставка по Рязани и России</p></div>
      </div>
    </section>
    <section className="pdp-content"><article><p className="eyebrow">О растении</p><h2>Описание</h2><p>{product.description || "Описание готовится. Подробности можно уточнить у консультанта."}</p></article><article><p className="eyebrow">После покупки</p><h2>Уход</h2><p>{product.careInstructions || "Мы приложим рекомендации по поливу, освещению и пересадке к вашему заказу."}</p></article></section>
    {product.recommendations.length > 0 && <section className="pdp-related"><div><p className="eyebrow">Вам может понравиться</p><h2>Похожие растения</h2></div><div className="product-grid">{product.recommendations.map((item) => <a className="product-card related-card" href={`/product/${item.id}`} key={item.id}><div className="product-image"><img src={item.image} alt={item.name} /></div><div className="product-info"><p className="latin">{item.latin}</p><h3>{item.name}</h3><strong>{money(item.price)}</strong></div></a>)}</div></section>}
    <footer className="pdp-footer"><a className="brand" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></a><p>Рязань, Новосёлов, 40А · +7 915 615-11-00 · ежедневно 08:00–20:00</p></footer>
    {notice && <div className="toast">{notice}</div>}
  </main>;
}

function Header({ cartCount }: { cartCount: number }) {
  return <><div className="announcement"><span>Бережно упакуем каждое растение</span><span>Доставка по Рязани и всей России</span></div><header className="header"><a className="brand" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></a><nav className="desktop-nav"><a href="/#catalog">Каталог</a><a href="/#care">Уход</a><a href="/#delivery">Доставка</a></nav><div className="header-actions"><a className="account-button" href="/account"><span>◯</span><span>Кабинет</span></a><a className="cart-button" href="/?cart=1"><span>Корзина</span><b>{cartCount}</b></a></div></header></>;
}
