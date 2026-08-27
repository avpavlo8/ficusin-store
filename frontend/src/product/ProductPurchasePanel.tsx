import type { ReactNode } from "react";
import type { ProductDetail, ProductVariant } from "./types";
import { keyCharacteristics, money } from "./types";

export function ProductPurchasePanel({ product, variant, quantity, favorite, inCart, reviewComposer, onVariant, onQuantity, onFavorite, onBuy }: {
  product: ProductDetail; variant?: ProductVariant; quantity: number; favorite: boolean; inCart: boolean;
  reviewComposer: ReactNode;
  onVariant: (id: number) => void; onQuantity: (value: number) => void; onFavorite: () => void; onBuy: () => void;
}) {
  const isPlant = product.catalogSection === "plants";
  const available = Boolean(variant && variant.stock > 0);
  const facts = keyCharacteristics(product, variant);
  const limit = available ? Math.min(variant?.stock || 1, 20) : 20;
  return <aside className="pdp-summary" aria-label="Информация и покупка">
    <header className="pdp-identity">
      <div><h1>{product.name}</h1>{isPlant && product.latin?.trim() && <p className="latin">{product.latin}</p>}</div>
      {reviewComposer}
    </header>
    <p className="pdp-lead">{product.shortDescription || product.description || (isPlant ? "Живое растение из каталога Фикусин." : "Товар из каталога Фикусин.")}</p>
    {product.variants.length > 0 && <fieldset className="variant-picker"><legend>{isPlant ? "Размер" : "Вариант"}</legend><div>{product.variants.map((item) => <button type="button" className={item.id === variant?.id ? "active" : ""} aria-pressed={item.id === variant?.id} onClick={() => onVariant(item.id)} key={item.id}><strong>{item.label}</strong><small>{item.stock > 0 ? money(item.price) : "Предзаказ"}</small></button>)}</div></fieldset>}
    <section className="pdp-commerce-box" aria-label="Покупка">
      <div className="pdp-price-row"><strong>{variant ? money(variant.price) : "Цена уточняется"}</strong><span className={available ? "stock-ok" : "stock-preorder"}>{available ? `В наличии${variant && variant.stock < 6 ? `: ${variant.stock} шт.` : ""}` : "Доступно по предзаказу"}</span></div>
      <div className="pdp-actions"><div className="pdp-quantity" aria-label="Количество"><button type="button" onClick={() => onQuantity(Math.max(1, quantity - 1))} disabled={quantity <= 1} aria-label="Уменьшить количество">−</button><output aria-live="polite">{quantity}</output><button type="button" onClick={() => onQuantity(Math.min(limit, quantity + 1))} disabled={!variant || quantity >= limit} aria-label="Увеличить количество">+</button></div><button type="button" className={inCart ? "pdp-cart-button in-cart" : "pdp-cart-button"} onClick={onBuy} disabled={!variant}>{inCart ? "В корзине — убрать" : available ? "Добавить в корзину" : "Оформить предзаказ"}</button><button type="button" className={favorite ? "pdp-favorite active" : "pdp-favorite"} onClick={onFavorite} aria-pressed={favorite} aria-label={favorite ? "Убрать из избранного" : "Добавить в избранное"}>{favorite ? "♥" : "♡"}</button></div>
      <ul className="pdp-assurances" aria-label="Гарантии"><li>Надёжная упаковка</li><li>Проверка перед отправкой</li><li>Бережная доставка</li></ul>
    </section>
    <div className="pdp-mobile-buybar" aria-label="Быстрая покупка">
      <strong>{variant ? money(variant.price) : "Цена уточняется"}</strong>
      <button type="button" onClick={onBuy} disabled={!variant}>{inCart ? "В корзине — убрать" : available ? "Добавить в корзину" : "Оформить предзаказ"}</button>
    </div>
    {facts.length > 0 && <section className="pdp-key-characteristics" aria-label="Ключевые характеристики"><h2>Главное о товаре</h2><dl>{facts.map((item) => <div key={item.code}><dt>{item.label}</dt><dd>{item.value}</dd></div>)}</dl></section>}
    {variant && <p className="pdp-sku">Артикул: {variant.sku}</p>}
  </aside>;
}
