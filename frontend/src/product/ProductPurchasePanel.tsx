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
    <div className="pdp-identity"><p className="latin">{product.latin}</p><h1>{product.name}</h1>
      {reviewComposer}
    </div>
    <p className="pdp-lead">{product.shortDescription || product.description || "Живое растение из каталога Фикусин. Перед отправкой проверим состояние и бережно упакуем."}</p>
    {product.variants.length > 1 && <fieldset className="variant-picker"><legend>Размер и комплектация</legend><div>{product.variants.map((item) => <button type="button" className={item.id === variant?.id ? "active" : ""} onClick={() => onVariant(item.id)} key={item.id} disabled={item.stock <= 0}><strong>{item.label}</strong><small>{item.stock > 0 ? money(item.price) : "Нет в наличии"}</small></button>)}</div></fieldset>}
    {variant && <dl className="pdp-specs">{variant.heightCm && <div><dt>Высота</dt><dd>{variant.heightCm} см</dd></div>}{variant.potDiameterCm && <div><dt>Горшок</dt><dd>Ø {variant.potDiameterCm} см</dd></div>}<div><dt>Артикул</dt><dd>{variant.sku}</dd></div><div><dt>Наличие</dt><dd className={available ? "stock-ok" : "stock-out"}>{available ? `${variant.stock} шт.` : "Нет"}</dd></div></dl>}
    {warnings.length > 0 && <div className="pdp-warnings" aria-label="Важные особенности">{warnings.map((warning) => <p key={warning}><span aria-hidden="true">!</span>{warning}</p>)}</div>}
    <div className="pdp-commerce-box"><div className="pdp-price-row"><strong>{variant ? money(variant.price) : "Цена уточняется"}</strong>{available && <span>В наличии · отправим после проверки</span>}</div>
      <div className="pdp-actions"><div className="pdp-quantity" aria-label="Количество"><button type="button" onClick={() => onQuantity(Math.max(1, quantity - 1))} disabled={quantity <= 1} aria-label="Уменьшить количество">−</button><output>{quantity}</output><button type="button" onClick={() => onQuantity(Math.min(Math.min(variant?.stock || 1, 20), quantity + 1))} disabled={!variant || quantity >= Math.min(variant.stock, 20)} aria-label="Увеличить количество">+</button></div><button className={inCart ? "pdp-cart-button in-cart" : "pdp-cart-button"} onClick={onBuy} disabled={!available}>{!available ? "Нет в наличии" : inCart ? "Обновить корзину" : "Добавить в корзину"}</button><button className={favorite ? "pdp-favorite active" : "pdp-favorite"} onClick={onFavorite} aria-label={favorite ? "Убрать из избранного" : "Добавить в избранное"}>{favorite ? "♥" : "♡"}</button></div>
      <div className="pdp-fulfillment"><div><span aria-hidden="true">✓</span><p><strong>Проверим растение</strong><small>Отбираем здоровый экземпляр перед передачей</small></p></div><div><span aria-hidden="true">⌂</span><p><strong>Упакуем по погоде</strong><small>Защитим грунт, крону и горшок</small></p></div><div><span aria-hidden="true">→</span><p><strong>Доставка по России</strong><small>Стоимость и срок будут в корзине</small></p></div></div>
    </div>
  </aside>;
}
