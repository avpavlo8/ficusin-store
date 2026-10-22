import { ProductImage } from "./ProductImage";
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { StoreHeader, type HeaderMenuItem, useStoreUser } from "./StoreHeader";
import { ProductGallery } from "./product/ProductGallery";
import { ProductPurchasePanel } from "./product/ProductPurchasePanel";
import { PlantAbout, PlantCareGuide, PlantQuestions } from "./product/PlantCareGuide";
import { ProductReviews, ReviewComposer } from "./product/ProductReviews";
import type { ProductDetail } from "./product/types";
import { attributeValue, money } from "./product/types";
import { useSharedCart } from "./lib/cart";
import { orderStorefrontCategories } from "./lib/category-order";
import { track } from "./lib/analytics";
import { useFavorites } from "./lib/favorites";

const plantOnlyAttributeCodes = new Set(["plant_type","height_cm","pot_diameter_cm","light_level","watering","humidity","care_level","toxicity","pet_safety","placement","growth_habit","height_class","flowering"]);
const PdpAdminTools = lazy(() => import("./PdpAdminTools").then((module) => ({ default: module.PdpAdminTools })));

export default function ProductPage({ slug }: { slug: string }) {
  const [categories, setCategories] = useState<Array<{ id:number; parentId:number|null; name:string; slug:string; sortOrder:number }>>([]);
  const [product, setProduct] = useState<ProductDetail | null>(null);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [activeImage, setActiveImage] = useState(0);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [quantity, setQuantity] = useState(1);
  const [revision, setRevision] = useState(0);

  const relatedTrack = useRef<HTMLDivElement>(null);
  const [relatedEdges, setRelatedEdges] = useState({ start: true, end: false });
  useEffect(() => {
    const track = relatedTrack.current;
    if (!track) return;
    const update = () => setRelatedEdges({ start: track.scrollLeft < 2, end: track.scrollLeft + track.clientWidth >= track.scrollWidth - 2 });
    update();
    const observer = new ResizeObserver(update);
    observer.observe(track);
    track.addEventListener("scroll", update, { passive: true });
    return () => { observer.disconnect(); track.removeEventListener("scroll", update); };
  }, [product]);
  const adminUser = useStoreUser();
  const [favorites, setFavorites] = useFavorites();
  const [cart, setCart] = useSharedCart();
  const cartCount = Object.values(cart).reduce((sum, value) => sum + value, 0);

  useEffect(() => {
    fetch(`/api/v1/products/${encodeURIComponent(slug)}`, { cache: "no-store" })
      .then(async (response) => { const body = await response.json() as { product?: ProductDetail; error?: string }; if (!response.ok || !body.product) throw new Error(body.error || "Товар не найден"); return body.product; })
      .then((item) => {
        const normalized = { ...item, passport: item.passport || {}, importantWarnings: item.importantWarnings || [], attributes: item.attributes || [], variants: (item.variants || []).map((variant) => ({ ...variant, images: variant.images || [], attributes: variant.attributes || [] })).sort((a, b) => (a.potDiameterCm || Number(a.label.match(/\d+(?:[.,]\d+)?/)?.[0]?.replace(",", ".")) || Infinity) - (b.potDiameterCm || Number(b.label.match(/\d+(?:[.,]\d+)?/)?.[0]?.replace(",", ".")) || Infinity)), reviews: (item.reviews || []).map((review) => ({ ...review, photos: review.photos || [], media: review.media || [] })), rating: Number(item.rating) || 0, reviewsCount: Number(item.reviewsCount) || 0 };
        const requestedSKU=new URLSearchParams(window.location.search).get("sku");
        setProduct(normalized); setSelectedID(normalized.variants.find((variant)=>variant.sku===requestedSKU)?.id ?? normalized.variants[0]?.id ?? null); setActiveImage(0);
		const viewed=normalized.variants.find((variant)=>variant.sku===requestedSKU) ?? normalized.variants[0];
		track("view_item", { productCode: normalized.id, sku: viewed?.sku, value: viewed?.price, properties: { name: normalized.name, category: normalized.catalogSection } });
      })
      .catch((reason) => setError(reason instanceof Error ? reason.message : "Не удалось загрузить товар"));
  }, [slug, revision]);

  useEffect(() => { fetch("/api/v1/categories").then((response) => response.json()).then((body: { categories?: typeof categories }) => setCategories(body.categories || [])).catch(() => setCategories([])); }, []);

  const headerMenus = useMemo(() => {
    const children = new Map<number|null,typeof categories>();
    categories.forEach((item) => children.set(item.parentId,[...(children.get(item.parentId)||[]),item]));
    const order = orderStorefrontCategories<typeof categories[number]>;
    const branch = (item:typeof categories[number]):HeaderMenuItem => ({id:item.id,label:item.name,slug:item.slug,children:order(children.get(item.id)||[]).map(branch)});
    const catalog = order(children.get(null)||[]).map(branch);
    const plantRoot = categories.find((item)=>item.parentId==null&&/растен/i.test(item.name));
    const leaves = (parentId:number):typeof categories => order(children.get(parentId)||[]).flatMap((item)=>children.get(item.id)?.length?leaves(item.id):[item]);
    return {catalog,plants:plantRoot?leaves(plantRoot.id).map((item)=>({id:item.id,label:item.name,slug:item.slug})):[]};
  },[categories]);

  const variant = useMemo(() => product?.variants.find((item) => item.id === selectedID) || product?.variants[0], [product, selectedID]);

  useEffect(() => {
    if (!variant) return;
    const stored = cart[variant.sku];
    if (!stored) return;
    const timer = window.setTimeout(() => setQuantity(stored), 0);
    return () => window.clearTimeout(timer);
  }, [cart, variant]);

  useEffect(() => {
    if (!product) return;
    const followAnchor = () => {
      const id = decodeURIComponent(window.location.hash.slice(1));
      if (!id) return;
      const target = document.getElementById(id);
      if (target instanceof HTMLDetailsElement) target.open = true;
      target?.scrollIntoView({ block: "start" });
    };
    const frame = requestAnimationFrame(followAnchor);
    window.addEventListener("hashchange", followAnchor);
    return () => { cancelAnimationFrame(frame); window.removeEventListener("hashchange", followAnchor); };
  }, [product]);

  const toggleFavorite = () => {
    if (!product) return;
    setFavorites((current) => {
      const next = new Set(current);
      if (next.has(product.id)) next.delete(product.id); else next.add(product.id);
      return next;
    });
  };

  const toggleCart = () => {
    if (!variant) return;
    if (cart[variant.sku]) {
	  track("remove_from_cart", { productCode: product?.id, sku: variant.sku, value: variant.price, quantity: cart[variant.sku], properties: { name: product?.name, category: product?.catalogSection } });
      setCart((current) => { const next = { ...current }; delete next[variant.sku]; return next; });
      setNotice("Товар удалён из корзины"); window.setTimeout(() => setNotice(""), 1800);
      return;
    }
	track("add_to_cart", { productCode: product?.id, sku: variant.sku, value: variant.price, quantity, properties: { name: product?.name, category: product?.catalogSection } });
    setCart((current) => ({ ...current, [variant.sku]: variant.stock > 0 ? Math.min(variant.stock, quantity) : quantity }));
    setNotice("Товар добавлен в корзину"); window.setTimeout(() => setNotice(""), 1800);
  };
  const changeQuantity = (value: number) => {
    setQuantity(value);
    if (!variant || !cart[variant.sku]) return;
    setCart((current) => ({ ...current, [variant.sku]: value }));
  };

  if (error) return <main className="product-page"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} /><section className="pdp-error"><h1>{error}</h1><a href="/#catalog">Вернуться в каталог</a></section></main>;
  if (!product) return <main className="product-page"><StoreHeader cartCount={cartCount} favoritesCount={favorites.size} />
    <nav className="breadcrumbs pdp-loading-breadcrumb" aria-label="Хлебные крошки"><span /></nav>
    <section className="pdp-main pdp-loading-main" aria-label="Загружаем карточку товара" aria-busy="true">
      <div className="pdp-loading-gallery" />
      <div className="pdp-loading-summary"><span /><span /><span /><span /><span /></div>
    </section>
  </main>;

  const customerAttributes = [...product.attributes, ...(variant?.attributes || [])].filter((item) => item.showInCharacteristics !== false && (product.catalogSection === "plants" || !plantOnlyAttributeCodes.has(item.code)));
  const category = categories.find(item => item.id === product.categoryId);
  const gallery = variant?.images?.length ? variant.images : product.images;
  const isPlant = product.catalogSection === "plants";
  const hasPlantFAQ = product.passport.faq?.some((item) => item.question.trim() && item.answer.trim()) ?? false;
  const tabs = isPlant
    ? ([['about','О растении'],['care','Уход'],['reviews','Отзывы'],['questions','Вопросы']] as const).filter(([id]) => id !== 'questions' || hasPlantFAQ)
    : ([['characteristics','Характеристики'],['reviews','Отзывы']] as const);
  const sectionID = (id: string) => ({ about: "plant-about", care: "plant-passport", reviews: "reviews", questions: "questions", characteristics: "characteristics" })[id];
  const openReviews = () => { document.getElementById("reviews")?.scrollIntoView({ behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth", block: "start" }); };
  const relatedPlants = product.recommendations.length > 0 && <section className="pdp-related"><header><div><h2>Похожие растения</h2></div></header><div className="pdp-related-carousel"><button type="button" className="pdp-related-side prev" disabled={relatedEdges.start} onClick={()=>relatedTrack.current?.scrollBy({left:-relatedTrack.current.clientWidth*.8,behavior:window.matchMedia('(prefers-reduced-motion: reduce)').matches?'instant':'smooth'})} aria-label="Предыдущие похожие растения">←</button><div className="pdp-related-track" ref={relatedTrack}>{product.recommendations.map((item) => <a className="product-card related-card" href={`/product/${item.id}`} onClick={()=>track("select_item",{productCode:item.id,value:item.price,properties:{list:"recommendations"}})} key={item.id}><div className="product-image"><ProductImage src={item.image} alt={item.name} /></div><div className="product-info"><p className="latin">{item.latin}</p><h3>{item.name}</h3><strong>{money(item.price)}</strong></div></a>)}</div><button type="button" className="pdp-related-side next" disabled={relatedEdges.end} onClick={()=>relatedTrack.current?.scrollBy({left:relatedTrack.current.clientWidth*.8,behavior:window.matchMedia('(prefers-reduced-motion: reduce)').matches?'instant':'smooth'})} aria-label="Следующие похожие растения">→</button></div></section>;

  return <main className="product-page">
    <StoreHeader cartCount={cartCount} favoritesCount={favorites.size} homeNavigation catalogMenuItems={headerMenus.catalog} plantMenuItems={headerMenus.plants} />
    <nav className="breadcrumbs" aria-label="Хлебные крошки"><a href="/">Главная</a><span>/</span><a href="/#catalog">Каталог</a><span>/</span>{category && <><a href={category.slug ? `/catalog/${encodeURIComponent(category.slug)}` : `/?category=${category.id}#catalog`}>{category.name}</a><span>/</span></>}<b>{product.name}</b></nav>
    {(adminUser?.adminRole === "owner" || adminUser?.adminRole === "administrator" || adminUser?.adminRole === "manager") && <Suspense fallback={null}><PdpAdminTools slug={slug} adminRole={adminUser.adminRole} onChanged={() => setRevision((value) => value + 1)} /></Suspense>}
    <section className="pdp-main">
      <ProductGallery images={gallery} name={product.name} active={Math.min(activeImage, Math.max(gallery.length - 1, 0))} onSelect={setActiveImage} />
      <ProductPurchasePanel product={product} variant={variant} quantity={quantity} favorite={favorites.has(product.id)} inCart={Boolean(variant && cart[variant.sku])} relatedPlants={relatedPlants} reviewComposer={<ReviewComposer slug={slug} rating={product.rating} count={product.reviewsCount} onOpenReviews={openReviews} />} onVariant={(id) => { const selected=product.variants.find((item)=>item.id===id); setSelectedID(id); setQuantity(1); setActiveImage(0); if(selected){const next=new URL(window.location.href);next.searchParams.set("sku",selected.sku);history.replaceState(null,"",next);} }} onQuantity={changeQuantity} onFavorite={toggleFavorite} onBuy={toggleCart} />
    </section>
    <div className={`pdp-tabs-shell${isPlant ? " plant-tabs-shell" : ""}`}><nav className="pdp-anchor-nav" aria-label="Разделы товара">{tabs.map(([id,label])=><a href={`#${sectionID(id)}`} key={id}>{label}{id==='reviews'&&product.reviewsCount>0&&<span>{product.reviewsCount}</span>}</a>)}</nav>
      <section className="pdp-tab-panel">
        {isPlant&&<PlantAbout product={product}/>}
        {isPlant&&<PlantCareGuide product={product}/>}
        {!isPlant&&<section className="pdp-characteristics-panel pdp-info-card" id="characteristics"><header className="pdp-section-heading"><h2>Характеристики</h2><p>Параметры выбранного варианта и общая информация о товаре</p></header><dl>{customerAttributes.length>0?customerAttributes.map((item)=><div key={`${item.code}-${variant?.sku || 'product'}`}><dt>{item.name}</dt><dd>{attributeValue(item.value,item.unit)}</dd></div>):<><div><dt>Освещение</dt><dd>{attributeValue(product.lightLevel||product.passport.lighting||'Не указано')}</dd></div><div><dt>Полив</dt><dd>{attributeValue(product.watering||product.passport.watering||'Не указано')}</dd></div><div><dt>Уровень ухода</dt><dd>{attributeValue(product.careLevel||product.passport.careDifficulty||'Не указано')}</dd></div><div><dt>Безопасность для питомцев</dt><dd>{attributeValue(product.petSafety||product.passport.toxicity||'Не указано')}</dd></div></>}</dl></section>}
        <ProductReviews reviews={product.reviews}/>
        {isPlant&&hasPlantFAQ&&<PlantQuestions product={product}/>}
      </section>
    </div>

    {variant && <div className="pdp-mobile-purchase" aria-label="Быстрая покупка"><div><strong>{money(variant.price)}</strong><small>{variant.stock > 0 ? "В наличии" : "Под заказ · цена ориентировочная"}</small></div>{cart[variant.sku] ? <a href="/cart">В корзину →</a> : <button type="button" onClick={toggleCart}>{variant.stock > 0 ? "В корзину" : "Заказать"}</button>}</div>}
    {notice && <div className="toast">{notice}</div>}
  </main>;
}
