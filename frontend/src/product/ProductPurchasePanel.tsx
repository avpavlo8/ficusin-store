import type { ReactNode } from "react";
import type { ProductDetail, ProductVariant } from "./types";
import { money } from "./types";

export function ProductPurchasePanel({ product, variant, quantity, favorite, inCart, warnings, reviewComposer, onVariant, onQuantity, onFavorite, onBuy }: {
  product: ProductDetail; variant?: ProductVariant; quantity: number; favorite: boolean; inCart: boolean; warnings: string[];
  reviewComposer: ReactNode;
  onVariant: (id: number) => void; onQuantity: (value: number) => void; onFavorite: () => void; onBuy: () => void;
}) {
  const available = Boolean(variant && variant.stock > 0);
  return <aside className="pdp-summary" aria-label="Покупка товара">
    <div className="pdp-identity"><h1>{product.name}</h1><p className="latin">{product.latin}</p>{reviewComposer}</div>
    <div className="pdp-purchase-layout">
      <div className="pdp-order-column">
        <p className="pdp-lead">{product.shortDescription || product.description || "Живое растение из каталога Фикусин. Перед отправкой проверим состояние и бережно упакуем."}</p>
        {product.variants.length > 0 && <fieldset className="variant-picker"><legend>Размер растения</legend><div>{product.variants.map((item) => <button type="button" className={item.id === variant?.id ? "active" : ""} onClick={() => onVariant(item.id)} key={item.id} disabled={item.stock <= 0}><strong>{item.potDiameterCm ? `Ø ${item.potDiameterCm} см` : item.label}</strong>{product.variants.length > 1 && <small>{item.stock > 0 ? money(item.price) : "Нет в наличии"}</small>}</button>)}</div></fieldset>}
        {variant && <dl className="pdp-specs">{variant.heightCm && <div><dt>Высота</dt><dd>{variant.heightCm} см</dd></div>}<div><dt>Артикул</dt><dd>{variant.sku}</dd></div></dl>}
        <div className="pdp-commerce-box"><div className="pdp-price-row"><strong>{variant ? money(variant.price) : "Цена уточняется"}</strong><span className={available ? "stock-ok" : "stock-out"}>{available ? "● В наличии" : "Под заказ"}</span></div>
          <div className="pdp-actions"><div className="pdp-quantity" aria-label="Количество"><button type="button" onClick={() => onQuantity(Math.max(1, quantity - 1))} disabled={quantity <= 1} aria-label="Уменьшить количество">−</button><output>{quantity}</output><button type="button" onClick={() => onQuantity(Math.min(Math.min(variant?.stock || 1, 20), quantity + 1))} disabled={!variant || quantity >= Math.min(variant.stock, 20)} aria-label="Увеличить количество">+</button></div><button className={inCart ? "pdp-cart-button in-cart" : "pdp-cart-button"} onClick={onBuy} disabled={!available}>{!available ? "Нет в наличии" : inCart ? "Обновить корзину" : "В корзину"}</button><button className={favorite ? "pdp-favorite active" : "pdp-favorite"} onClick={onFavorite} aria-label={favorite ? "Убрать из избранного" : "Добавить в избранное"}>{favorite ? "♥" : "♡"}</button></div>
        </div>
      </div>
      <div className="pdp-benefits"><div><span aria-hidden="true">♧</span><p><strong>{product.careLevel === "demanding" ? "Требует внимания" : "Легко ухаживать"}</strong><small>Подойдёт даже новичкам</small></p></div><div><span aria-hidden="true">☼</span><p><strong>Нужен яркий свет</strong><small>Лучше растёт в светлом месте</small></p></div><div><span aria-hidden="true">↕</span><p><strong>Характеристики растения</strong><small>{variant?.potDiameterCm ? `Ø ${variant.potDiameterCm} см` : variant?.label}{variant?.heightCm ? ` · высота ${variant.heightCm} см` : ""}{warnings[0] ? ` · ${warnings[0]}` : ""}</small></p></div></div>
    </div>
  </aside>;
}
