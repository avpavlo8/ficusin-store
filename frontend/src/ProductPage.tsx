import { useEffect, useMemo, useRef, useState } from "react";
import { StoreHeader, STORAGE_EVENT, type HeaderMenuItem, useStoreUser } from "./StoreHeader";
import { PdpAdminTools } from "./PdpAdminTools";
import { ProductGallery } from "./product/ProductGallery";
import { ProductPurchasePanel } from "./product/ProductPurchasePanel";
import { ProductReviews, ReviewComposer } from "./product/ProductReviews";
import type { ProductDetail } from "./product/types";
import { attributeValue, money } from "./product/types";
import { useSharedCart } from "./lib/cart";

export default function ProductPage({ slug }: { slug: string }) {
  const [categories, setCategories] = useState<Array<{ id:number; parentId:number|null; name:string; sortOrder:number }>>([]);
  const [product, setProduct] = useState<ProductDetail | null>(null);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [activeImage, setActiveImage] = useState(0);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [quantity, setQuantity] = useState(1);
  const [revision, setRevision] = useState(0);
  const [activeTab, setActiveTab] = useState<"care"|"characteristics"|"reviews"|"questions">("care");
  const relatedTrack = useRef<HTMLDivElement>(null);
  const adminUser = useStoreUser();
  const [favorites, setFavorites] = useState<Set<string>>(() => {
    try { return new Set(JSON.parse(localStorage.getItem("ficusin-favorites") || "[]") as string[]); }
    catch { return new Set(); }
  });
  const [cart, setCart] = useSharedCart();
  const cartCount = Object.values(cart).reduce((sum, value) => sum + value, 0);

  useEffect(() => {
    fetch(`/api/v1/products/${encodeURIComponent(slug)}`, { cache: "no-store" })
      .then(async (response) => { const body = await response.json() as { product?: ProductDetail; error?: string }; if (!response.ok || !body.product) throw new Error(body.error || "Товар не найден"); return body.product; })
      .then((item) => {
        const normalized = { ...item, passport: item.passport || {}, importantWarnings: item.importantWarnings || [], attributes: item.attributes || [], variants: (item.variants || []).map((variant) => ({ ...variant, attributes: variant.attributes || [] })), reviews: item.reviews || [], rating: Number(item.rating) || 0, reviewsCount: Number(item.reviewsCount) || 0 };
        setProduct(normalized); setSelectedID(normalized.variants[0]?.id ?? null); setActiveImage(0);
        document.title = `${normalized.name} — Фикусин`;
      })
      .catch((reason) => setError(reason instanceof Error ? reason.message : "Не удалось загрузить товар"));
  }, [slug, revision]);

  useEffect(() => { fetch("/api/v1/categories").then((response) => response.json()).then((body: { categories?: typeof categories }) => setCategories(body.categories || [])).catch(() => setCategories([])); }, []);

  const headerMenus = useMemo(() => {
    const children = new Map<number|null,typeof categories>();
    categories.forEach((item) => children.set(item.parentId,[...(children.get(item.parentId)||[]),item]));
    const order = (items:typeof categories) => [...items].sort((a,b)=>a.sortOrder-b.sortOrder||a.name.localeCompare(b.name,"ru"));
    const catalog:HeaderMenuItem[] = order(children.get(null)||[]).map((item)=>({id:item.id,label:item.name}));
    const plantRoot = categories.find((item)=>item.parentId==null&&/растен/i.test(item.name));
    const leaves = (parentId:number):typeof categories => order(children.get(parentId)||[]).flatMap((item)=>children.get(item.id)?.length?leaves(item.id):[item]);
    return {catalog,plants:plantRoot?leaves(plantRoot.id).map((item)=>({id:item.id,label:item.name})):[]};
  },[categories]);

  const variant = useMemo(() => product?.variants.find((item) => item.id === selectedID) || product?.variants[0], [product, selectedID]);

  useEffect(() => {
    if (!variant) return;
    const stored = cart[variant.sku];
    if (!stored) return;
    const timer = window.setTimeout(() => setQuantity(stored), 0);
    return () => window.clearTimeout(timer);
  }, [cart, variant]);

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

  const toggleCart = () => {
    if (!variant) return;
    if (cart[variant.sku]) {
      setCart((current) => { const next = { ...current }; delete next[variant.sku]; return next; });
      setNotice("Товар удалён из корзины"); window.setTimeout(() => setNotice(""), 1800);
      return;
    }
    setCart((current) => ({ ...current, [variant.sku]: variant.stock > 0 ? Math.min(variant.stock, quantity) : quantity }));
    setNotice("Товар добавлен в корзину"); window.setTimeout(() => setNotice(""), 1800);
  };
  const changeQuantity = (value: number) => {
    setQuantity(value);
    if (!variant || !cart[variant.sku]) return;
    setCart((current) => ({ ...current, [variant.sku]: value }));
  };

  if (error) return <main className="product-page"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} /><section className="pdp-error"><h1>{error}</h1><a href="/#catalog">Вернуться в каталог</a></section></main>;
  if (!product) return <main className="product-page"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} /><section className="pdp-error"><p>Загружаем карточку товара…</p></section></main>;

  const customerAttributes = [...product.attributes, ...(variant?.attributes || [])].filter((item) => item.showInCharacteristics !== false);

  return <main className="product-page">
    <StoreHeader cartCount={cartCount} favoritesCount={favorites.size} homeNavigation catalogMenuItems={headerMenus.catalog} plantMenuItems={headerMenus.plants} onHomeCategoryPick={() => { window.location.href="/#catalog"; }} />
    <nav className="breadcrumbs" aria-label="Хлебные крошки"><a href="/">Главная</a><span>/</span><a href="/#catalog">Каталог</a><span>/</span><b>{product.name}</b></nav>
    <PdpAdminTools slug={slug} adminRole={adminUser?.adminRole} onChanged={() => setRevision((value) => value + 1)} />
    <section className="pdp-main">
      <ProductGallery images={product.images} name={product.name} active={activeImage} onSelect={setActiveImage} />
      <ProductPurchasePanel product={product} variant={variant} quantity={quantity} favorite={favorites.has(product.id)} inCart={Boolean(variant && cart[variant.sku])} reviewComposer={<ReviewComposer slug={slug} rating={product.rating} count={product.reviewsCount} />} onVariant={(id) => { setSelectedID(id); setQuantity(1); }} onQuantity={changeQuantity} onFavorite={toggleFavorite} onBuy={toggleCart} />
    </section>
    <div className="pdp-tabs-shell"><nav className="pdp-anchor-nav" aria-label="Разделы товара">{([['care','О растении'],['characteristics','Характеристики'],['reviews','Отзывы'],['questions','Вопросы']] as const).map(([id,label])=><button type="button" className={activeTab===id?'active':''} onClick={()=>setActiveTab(id)} aria-selected={activeTab===id} key={id}>{label}{id==='reviews'&&product.reviewsCount>0&&<span>{product.reviewsCount}</span>}</button>)}</nav>
      <section className="pdp-tab-panel" aria-live="polite">
        {activeTab==='care'&&<section className="pdp-content pdp-section pdp-info-card" id="about"><header className="pdp-section-heading"><h2>Уход за растением</h2></header><div><article><h3>О растении</h3><p>{product.description || "Описание готовится. Подробности можно уточнить у консультанта."}</p></article><article><h3>Базовый уход</h3><p>{product.careInstructions || "Мы приложим рекомендации по поливу, освещению и пересадке к вашему заказу."}</p></article></div></section>}
        {activeTab==='characteristics'&&<section className="pdp-characteristics-panel pdp-info-card" id="characteristics"><header className="pdp-section-heading"><h2>Характеристики</h2><p>Параметры выбранного варианта и общая информация о товаре</p></header><dl>{customerAttributes.length>0?customerAttributes.map((item)=><div key={`${item.code}-${variant?.sku || 'product'}`}><dt>{item.name}</dt><dd>{attributeValue(item.value,item.unit)}</dd></div>):<><div><dt>Освещение</dt><dd>{attributeValue(product.lightLevel||product.passport.lighting||'Не указано')}</dd></div><div><dt>Полив</dt><dd>{attributeValue(product.watering||product.passport.watering||'Не указано')}</dd></div><div><dt>Уровень ухода</dt><dd>{attributeValue(product.careLevel||product.passport.careDifficulty||'Не указано')}</dd></div><div><dt>Безопасность для питомцев</dt><dd>{attributeValue(product.petSafety||product.passport.toxicity||'Не указано')}</dd></div></>}</dl></section>}
        {activeTab==='reviews'&&<ProductReviews reviews={product.reviews}/>}
        {activeTab==='questions'&&<section className="pdp-questions pdp-info-card" id="questions"><header className="pdp-section-heading"><h2>Вопросы о растении</h2></header>{(product.passport.faq||[]).length?product.passport.faq!.map((item,index)=><details key={`${item.question}-${index}`}><summary>{item.question}</summary><p>{item.answer}</p></details>):<div className="pdp-question-empty"><strong>Остались вопросы?</strong><p>Напишите нам — подскажем по уходу, размеру и доставке.</p><a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Задать вопрос →</a></div>}</section>}
      </section>
    </div>
    {product.recommendations.length > 0 && <section className="pdp-related"><header><div><p className="eyebrow">Вам может понравиться</p><h2>Похожие растения</h2></div></header><div className="pdp-related-carousel"><button type="button" className="pdp-related-side prev" onClick={()=>relatedTrack.current?.scrollBy({left:-relatedTrack.current.clientWidth*.8,behavior:'smooth'})} aria-label="Предыдущие похожие растения">←</button><div className="pdp-related-track" ref={relatedTrack}>{product.recommendations.map((item) => <a className="product-card related-card" href={`/product/${item.id}`} key={item.id}><div className="product-image"><img src={item.image} alt={item.name} /></div><div className="product-info"><p className="latin">{item.latin}</p><h3>{item.name}</h3><strong>{money(item.price)}</strong><span className="related-arrow" aria-hidden="true">→</span></div></a>)}</div><button type="button" className="pdp-related-side next" onClick={()=>relatedTrack.current?.scrollBy({left:relatedTrack.current.clientWidth*.8,behavior:'smooth'})} aria-label="Следующие похожие растения">→</button></div></section>}
    {notice && <div className="toast">{notice}</div>}
  </main>;
}
