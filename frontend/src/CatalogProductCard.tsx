import { catalogSize } from "./lib/storefront-format";
import { CareLevel } from "./product/CareLevel";
import { money } from "./product/types";
import { openSavedCart } from "./lib/cart";

export type CatalogCardProduct = {
  id: string; sku: string; name: string; latin: string; price: number;
  image: string; size: string; stock?: number; heightCm?: number;
  catalogSection: string; careLevel?: string; rating?: number; reviewsCount?: number;
  filterAttributes?: Array<{ code: string; value: unknown }>;
};

export function CatalogProductCard({ product, inCart, favorite, onFavorite, onAdd, onDecrease, onSelect, preserveSKU }: {
  product: CatalogCardProduct; inCart: number; favorite: boolean;
  onFavorite: () => void; onAdd: () => void; onDecrease: () => void; onSelect?: () => void;
  preserveSKU?: boolean;
}) {
  const preorder = (product.stock ?? 0) <= 0;
  const maximum = product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20;
  const cartIcon = <svg className="catalog-cart-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M4.5 7.5h2l1.4 9.2h9.8l1.8-6.5H7.1M9.5 20a1 1 0 1 0 0-2 1 1 0 0 0 0 2Zm7 0a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z" /></svg>;
  const pot = product.size?.match(/[dдø]\s*(\d+(?:[.,]\d+)?)/i)?.[1];
  const href = `/product/${product.id}${preserveSKU ? `?sku=${encodeURIComponent(product.sku)}` : ""}`;
  return <article className={preorder ? "storefront-card preorder" : "storefront-card"}>
    <a className="storefront-image" href={href} onClick={onSelect}><img src={product.image} alt={product.name} loading="lazy" /></a>
    <a className="storefront-name" href={href} onClick={onSelect}>{product.name}</a>
    {product.catalogSection === "plants" ? <div className="storefront-plant-specs">
      <span className="plant-spec-row"><strong>Высота:</strong> {product.heightCm != null && product.heightCm > 0 ? `${product.heightCm} см` : "Не указана"}</span>
      <span className="plant-spec-row"><strong>Горшок:</strong> {pot ? `D${pot}` : "Не указан"}</span>
      <span className="plant-spec-row"><strong>Уход:</strong> <CareLevel shortLabel value={String(product.filterAttributes?.find(attribute => attribute.code === "care_level")?.value || product.careLevel || "")} />{!product.careLevel && !product.filterAttributes?.some(attribute => attribute.code === "care_level" && attribute.value) && "Не указан"}</span>
    </div> : <p className="storefront-size">{catalogSize(product.size)}</p>}
    <div className="storefront-buy with-favorite">
      <span className="storefront-price"><strong aria-label={preorder ? `Ориентировочная цена: ${money(product.price)}` : undefined}>{preorder ? "≈ " : ""}{money(product.price)}</strong>{preorder && <em>Нет в наличии</em>}</span>
      <span className="storefront-rating" role={(product.reviewsCount ?? 0) > 0 ? "img" : undefined} aria-hidden={(product.reviewsCount ?? 0) <= 0 || undefined} aria-label={(product.reviewsCount ?? 0) > 0 ? `Оценка ${(product.rating ?? 0).toFixed(1)} из 5, отзывов: ${product.reviewsCount}` : undefined}>{(product.reviewsCount ?? 0) > 0 && <>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 2.8 2.8 5.7 6.3.9-4.6 4.5 1.1 6.3-5.6-3-5.6 3 1.1-6.3L2.9 9.4l6.3-.9L12 2.8Z" /></svg><b>{(product.rating ?? 0).toFixed(1)}</b>
      </>}</span>
      <button type="button" className={favorite ? "storefront-fav active" : "storefront-fav"} onClick={onFavorite} aria-label={favorite ? "Убрать из избранного" : "В избранное"} aria-pressed={favorite}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z" /></svg>
      </button>

      {inCart > 0 ? <div className="catalog-cart-controls">
        <div className="catalog-card-quantity" role="group" aria-label={`Количество: ${product.name}`}>
          <button type="button" onClick={onDecrease} aria-label={`Уменьшить количество: ${product.name}`}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 12h12" /></svg></button>
          <output className="cart-item-count" aria-live="polite" aria-label="Количество в корзине">{inCart}</output>
          <button type="button" onClick={onAdd} disabled={inCart >= maximum} aria-label={`Увеличить количество: ${product.name}`}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 12h12M12 6v12" /></svg></button>
        </div>
        <button type="button" className="catalog-cart-open" onClick={() => { void openSavedCart(); }} aria-label={`Перейти в корзину, ${inCart} шт.`} title="Открыть корзину">{cartIcon}</button>
      </div> : <button type="button" className={preorder ? "preorder-action" : undefined}
        onClick={onAdd} aria-label={preorder ? "Заказать" : "В корзину"}
        title={preorder ? "Заказать без оплаты — менеджер уточнит поставку" : `Добавить в корзину · ${catalogSize(product.size)}`}>
        {cartIcon}<span className="cart-button-label">{preorder ? "Заказать" : "В корзину"}</span>
      </button>}
    </div>
  </article>;
}
