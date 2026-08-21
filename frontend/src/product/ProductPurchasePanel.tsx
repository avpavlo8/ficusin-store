import type { ReactNode } from "react";
import type { ProductDetail, ProductVariant } from "./types";
import { attributeLabel, attributeValue, money } from "./types";

const characteristicIcons: Record<string, string> = {
  light: "M12 2v2m0 16v2M4.93 4.93l1.42 1.42m11.3 11.3 1.42 1.42M2 12h2m16 0h2M4.93 19.07l1.42-1.42m11.3-11.3 1.42-1.42M17 12a5 5 0 1 1-10 0 5 5 0 0 1 10 0Z",
  water: "M12 2s6 6.2 6 12a6 6 0 0 1-12 0c0-5.8 6-12 6-12Zm-3 12.5c.35 1.45 1.35 2.25 3 2.5",
  height: "M8 4 12 1l4 3m-4-3v22m-4-3 4 3 4-3",
  pot: "M6 9h12l-1.4 11H7.4L6 9Zm-1-3h14v3H5V6Z",
  care: "M19 4c-7.5.2-12 3.8-12 10 0 3 2 5 5 5 6.2 0 8.2-7.2 7-15ZM5 21c1.4-5.5 5.2-9.2 11-11",
  pets: "M8.5 10.5c-1.1 0-2-1.2-2-2.7s.9-2.8 2-2.8 2 1.3 2 2.8-.9 2.7-2 2.7Zm7 0c-1.1 0-2-1.2-2-2.7s.9-2.8 2-2.8 2 1.3 2 2.8-.9 2.7-2 2.7ZM5 15c-1 0-1.8-1-1.8-2.3S4 10.5 5 10.5s1.8 1 1.8 2.2S6 15 5 15Zm14 0c-1 0-1.8-1-1.8-2.3s.8-2.2 1.8-2.2 1.8 1 1.8 2.2S20 15 19 15Zm-7 6c-3.5 0-5.5-1.5-5.5-3.5 0-1.3 1-2.2 2.2-3.5 1-1 1.6-2 3.3-2s2.3 1 3.3 2c1.2 1.3 2.2 2.2 2.2 3.5 0 2-2 3.5-5.5 3.5Z",
  humidity: "M7 17c-2.2 0-4-1.7-4-3.8C3 10 7 6 7 6s4 4 4 7.2C11 15.3 9.2 17 7 17Zm10 4c-2.2 0-4-1.7-4-3.8C13 14 17 10 17 10s4 4 4 7.2c0 2.1-1.8 3.8-4 3.8Z",
};

function CharacteristicIcon({ name }: { name: keyof typeof characteristicIcons }) {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d={characteristicIcons[name]} /></svg>;
}

export function ProductPurchasePanel({ product, variant, quantity, favorite, inCart, reviewComposer, onVariant, onQuantity, onFavorite, onBuy }: {
  product: ProductDetail; variant?: ProductVariant; quantity: number; favorite: boolean; inCart: boolean;
  reviewComposer: ReactNode;
  onVariant: (id: number) => void; onQuantity: (value: number) => void; onFavorite: () => void; onBuy: () => void;
}) {
  const available = Boolean(variant && variant.stock > 0);
  const allAttributes = [...product.attributes, ...(variant?.attributes || [])];
  const diameter = variant?.potDiameterCm && variant.potDiameterCm > 0
    ? `${variant.potDiameterCm} см`
    : /^\d+(?:[.,]\d+)?$/.test(variant?.label?.trim() || "") ? `${variant!.label.trim()} см` : "Не указан";
  const sizeLabel = (item: ProductVariant) => item.potDiameterCm && item.potDiameterCm > 0
    ? `Ø ${item.potDiameterCm} см`
    : /^\d+(?:[.,]\d+)?$/.test(item.label.trim()) ? `Ø ${item.label.trim()} см` : item.label;
  const characteristic = (fallback: string, ...tokens: string[]) => {
    const item = allAttributes.find((entry) => tokens.some((token) => `${entry.code} ${entry.name}`.toLocaleLowerCase("ru").includes(token)));
    return item ? attributeValue(item.value, item.unit) : fallback;
  };
  return <aside className="pdp-summary" aria-label="Покупка товара">
    <div className="pdp-purchase-layout">
      <div className="pdp-order-column">
        <div className="pdp-identity"><h1>{product.name}</h1><p className="latin">{product.latin}</p>{reviewComposer}</div>
        <p className="pdp-lead">{product.shortDescription || product.description || "Живое растение из каталога Фикусин. Перед отправкой проверим состояние и бережно упакуем."}</p>
        <div className="pdp-parameters">
          {product.variants.length > 0 && <fieldset className="variant-picker"><legend>Размер растения</legend><div>{product.variants.map((item) => <button type="button" className={item.id === variant?.id ? "active" : ""} onClick={() => onVariant(item.id)} key={item.id}><strong>{sizeLabel(item)}</strong>{product.variants.length > 1 && <small>{item.stock > 0 ? money(item.price) : "Под заказ"}</small>}</button>)}</div></fieldset>}
          {variant && <dl className="pdp-specs">{variant.heightCm && <div><dt>Высота</dt><dd>{variant.heightCm} см</dd></div>}<div><dt>Артикул</dt><dd>{variant.sku}</dd></div></dl>}
        </div>
      </div>
      <div className="pdp-side-column">
        <div className="pdp-key-characteristics" aria-label="Основные характеристики">
          <h2>Характеристики растения</h2>
          <div><span className="characteristic-icon light"><CharacteristicIcon name="light" /></span><p><small>Освещение</small><strong>{characteristic(attributeLabel(product.lightLevel || product.passport.lighting || "Не указано"),"освещ","light")}</strong></p></div>
          <div><span className="characteristic-icon water"><CharacteristicIcon name="water" /></span><p><small>Полив</small><strong>{characteristic(attributeLabel(product.watering || product.passport.watering || "Не указано"),"полив","water")}</strong></p></div>
          <div><span className="characteristic-icon height"><CharacteristicIcon name="height" /></span><p><small>Высота</small><strong>{characteristic(variant?.heightCm ? `${variant.heightCm} см` : attributeLabel(product.heightClass || "Не указано"),"высот","height")}</strong></p></div>
          <div><span className="characteristic-icon pot"><CharacteristicIcon name="pot" /></span><p><small>Диаметр горшка</small><strong>{diameter !== "Не указан" ? diameter : characteristic("Не указан","диаметр","горш")}</strong></p></div>
          <div><span className="characteristic-icon care"><CharacteristicIcon name="care" /></span><p><small>Уход</small><strong>{characteristic(attributeLabel(product.careLevel || product.passport.careDifficulty || "Не указано"),"уход","сложност","care")}</strong></p></div>
          <div><span className="characteristic-icon pets"><CharacteristicIcon name="pets" /></span><p><small>Питомцы</small><strong>{characteristic(attributeLabel(product.petSafety || product.passport.toxicity || "Не указано"),"питом","безопас","токсич","pet")}</strong></p></div>
          <div><span className="characteristic-icon humidity"><CharacteristicIcon name="humidity" /></span><p><small>Влажность воздуха</small><strong>{characteristic(attributeLabel(product.passport.humidity || "Не указано"),"влажн","humidity")}</strong></p></div>
        </div>
        <div className="pdp-commerce-box"><div className="pdp-price-row"><strong>{variant ? money(variant.price) : "Цена уточняется"}</strong><span className={available ? "stock-ok" : "stock-out"}>{available ? "● В наличии" : "Под заказ"}</span></div>
          <div className="pdp-actions"><div className="pdp-quantity" aria-label="Количество"><button type="button" onClick={() => onQuantity(Math.max(1, quantity - 1))} disabled={quantity <= 1} aria-label="Уменьшить количество">−</button><output>{quantity}</output><button type="button" onClick={() => onQuantity(Math.min(available ? Math.min(variant?.stock || 1, 20) : 20, quantity + 1))} disabled={!variant || quantity >= (available ? Math.min(variant.stock, 20) : 20)} aria-label="Увеличить количество">+</button></div><button className={inCart ? "pdp-cart-button in-cart" : "pdp-cart-button"} onClick={onBuy} disabled={!variant}>{inCart ? "Удалить из корзины" : "В корзину"}</button><button className={favorite ? "pdp-favorite active" : "pdp-favorite"} onClick={onFavorite} aria-label={favorite ? "Убрать из избранного" : "Добавить в избранное"}>{favorite ? "♥" : "♡"}</button></div>
        </div>
      </div>
    </div>
  </aside>;
}
