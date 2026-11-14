import type { ReactNode } from "react";
import type { ProductDetail, ProductVariant } from "./types";
import { attributeLabel, displayAttributeValue, money } from "./types";
import { CareLevel } from "./CareLevel";

import { CharacteristicIcon } from "./CharacteristicIcon";

const plantOnlyAttributeCodes = new Set([
  "plant_type",
  "height_cm",
  "pot_diameter_cm",
  "light_level",
  "watering",
  "humidity",
  "care_level",
  "toxicity",
  "pet_safety",
  "placement",
  "growth_habit",
  "height_class",
  "flowering",
  "leaf_coloring",
  "fruiting",
  "wintering_type",
]);

export function ProductPurchasePanel({
  product,
  variant,
  quantity,
  favorite,
  inCart,
  reviewComposer,
  relatedPlants,
  onVariant,
  onQuantity,
  onFavorite,
  onBuy,
}: {
  product: ProductDetail;
  variant?: ProductVariant;
  quantity: number;
  favorite: boolean;
  inCart: boolean;
  reviewComposer: ReactNode;
  relatedPlants?: ReactNode;
  onVariant: (id: number) => void;
  onQuantity: (value: number) => void;
  onFavorite: () => void;
  onBuy: () => void;
}) {
  const available = Boolean(variant && variant.stock > 0);
  const isPlant = product.catalogSection === "plants";
  const allAttributes = [...product.attributes, ...(variant?.attributes || [])].filter(
    (item) => isPlant || !plantOnlyAttributeCodes.has(item.code),
  );
  const careAttribute = product.attributes.find((item) => item.code === "care_level");
  const careLevel = String(careAttribute?.value || product.careLevel || "");
  const hasCanonicalCareLabel =
    careAttribute &&
    (careAttribute.displayValue != null ||
      careAttribute.optionLabels?.[String(careAttribute.value)]);
  const diameter =
    variant?.potDiameterCm && variant.potDiameterCm > 0
      ? `${variant.potDiameterCm} см`
      : /^\d+(?:[.,]\d+)?$/.test(variant?.label?.trim() || "")
        ? `${variant!.label.trim()} см`
        : "Не указан";
  const sizeLabel = (item: ProductVariant) => {
    const label = item.label.trim();
    if (!item.potDiameterCm || item.potDiameterCm <= 0) {
      return /^\d+(?:[.,]\d+)?$/.test(label) ? `Ø ${label} см` : label;
    }
    const pot = `Ø ${item.potDiameterCm} см`;
    const diameterOnly = label.match(/^(?:D|Ø|⌀)?\s*(\d+(?:[.,]\d+)?)\s*(?:см)?$/i);
    if (
      !label ||
      /^Основной (?:вариант|размер)$/i.test(label) ||
      (diameterOnly && Number(diameterOnly[1].replace(",", ".")) === item.potDiameterCm)
    )
      return pot;
    const detail = label
      .replace(/(?:\s*[·,|]\s*|\s+)?(?:D|Ø|⌀)\s*\d+(?:[.,]\d+)?\s*(?:см)?$/i, "")
      .trim();
    return detail ? `${detail} · ${pot}` : pot;
  };
  const characteristic = (fallback: string, ...tokens: string[]) => {
    const item = allAttributes.find((entry) =>
      tokens.some((token) => `${entry.code} ${entry.name}`.toLocaleLowerCase("ru").includes(token)),
    );
    return item ? displayAttributeValue(item) : fallback;
  };
  const standardSummaryTokens = [
    "освещ",
    "light",
    "полив",
    "water",
    "высот",
    "height",
    "диаметр",
    "горш",
    "pot",
    "уход",
    "сложност",
    "care",
    "питом",
    "безопас",
    "токсич",
    "pet",
    "влажн",
    "humidity",
  ];
  const extraSummary = allAttributes
    .filter(
      (item) =>
        item.summaryPosition != null &&
        !standardSummaryTokens.some((token) =>
          `${item.code} ${item.name}`.toLocaleLowerCase("ru").includes(token),
        ),
    )
    .sort((a, b) => a.summaryPosition! - b.summaryPosition!);
  return (
    <aside className="pdp-summary" aria-label="Покупка товара">
      <div className="pdp-purchase-layout">
        <div className="pdp-order-column">
          <div className="pdp-identity">
            <h1>{product.name}</h1>
            <p className="latin">{product.latin}</p>
            {reviewComposer}
          </div>

          <div className="pdp-parameters">
            {product.variants.length > 0 && (
              <fieldset className="variant-picker">
                <legend>{isPlant ? "Размер растения" : "Вариант товара"}</legend>
                <div>
                  {product.variants.map((item) => (
                    <button
                      type="button"
                      className={item.id === variant?.id ? "active" : ""}
                      onClick={() => onVariant(item.id)}
                      key={item.id}
                    >
                      <strong>{isPlant ? sizeLabel(item) : item.label}</strong>
                      {product.variants.length > 1 && (
                        <small>{item.stock > 0 ? money(item.price) : "Под заказ"}</small>
                      )}
                    </button>
                  ))}
                </div>
              </fieldset>
            )}
            {Boolean(variant?.heightCm) && (
              <dl className="pdp-specs">
                <div>
                  <dt>Высота</dt>
                  <dd>{variant?.heightCm} см</dd>
                </div>
              </dl>
            )}
          </div>
          <div className="pdp-commerce-box">
            <div className="pdp-price-row">
              <strong
                aria-label={
                  variant && !available
                    ? `Ориентировочная цена: ${money(variant.price)}`
                    : undefined
                }
              >
                {variant ? `${available ? "" : "≈ "}${money(variant.price)}` : "Цена уточняется"}
              </strong>
              <span className={available ? "stock-ok" : "stock-out"}>
                {available ? "● В наличии" : "Под заказ"}
              </span>
            </div>
            <div className="pdp-actions">
              <div className="pdp-quantity" aria-label="Количество">
                <button
                  type="button"
                  onClick={() => onQuantity(Math.max(1, quantity - 1))}
                  disabled={quantity <= 1}
                  aria-label="Уменьшить количество"
                >
                  −
                </button>
                <output>{quantity}</output>
                <button
                  type="button"
                  onClick={() =>
                    onQuantity(
                      Math.min(available ? Math.min(variant?.stock || 1, 20) : 20, quantity + 1),
                    )
                  }
                  disabled={!variant || quantity >= (available ? Math.min(variant.stock, 20) : 20)}
                  aria-label="Увеличить количество"
                >
                  +
                </button>
              </div>
              <button
                className={inCart ? "pdp-cart-button in-cart" : "pdp-cart-button"}
                onClick={onBuy}
                disabled={!variant}
              >
                {inCart ? "Удалить из корзины" : "В корзину"}
              </button>
              <button
                className={favorite ? "pdp-favorite active" : "pdp-favorite"}
                onClick={onFavorite}
                aria-label={favorite ? "Убрать из избранного" : "Добавить в избранное"}
              >
                {favorite ? "♥" : "♡"}
              </button>
            </div>
          </div>
        </div>
        <div className="pdp-side-column">
          {isPlant ? (
            <div className="pdp-key-characteristics" aria-label="Основные характеристики">
              <h2>Характеристики растения</h2>
              <div>
                <span className="characteristic-icon light">
                  <CharacteristicIcon name="light" />
                </span>
                <p>
                  <small>Освещение</small>
                  <strong>
                    {characteristic(
                      attributeLabel(
                        product.lightLevel || product.passport.lighting || "Не указано",
                      ),
                      "освещ",
                      "light",
                    )}
                  </strong>
                </p>
              </div>
              <div>
                <span className="characteristic-icon water">
                  <CharacteristicIcon name="water" />
                </span>
                <p>
                  <small>Полив</small>
                  <strong>
                    {characteristic(
                      attributeLabel(product.watering || product.passport.watering || "Не указано"),
                      "полив",
                      "water",
                    )}
                  </strong>
                </p>
              </div>
              <div>
                <span className="characteristic-icon height">
                  <CharacteristicIcon name="height" />
                </span>
                <p>
                  <small>Высота</small>
                  <strong>
                    {characteristic(
                      variant?.heightCm
                        ? `${variant?.heightCm} см`
                        : attributeLabel(product.heightClass || "Не указано"),
                      "высот",
                      "height",
                    )}
                  </strong>
                </p>
              </div>
              <div>
                <span className="characteristic-icon pot">
                  <CharacteristicIcon name="pot" />
                </span>
                <p>
                  <small>Диаметр горшка</small>
                  <strong>
                    {diameter !== "Не указан"
                      ? diameter
                      : characteristic("Не указан", "диаметр", "горш")}
                  </strong>
                </p>
              </div>
              <div>
                <span className="characteristic-icon care">
                  <CharacteristicIcon name="care" />
                </span>
                <p>
                  <small>Сложность ухода</small>
                  <strong>
                    {hasCanonicalCareLabel ? (
                      displayAttributeValue(careAttribute)
                    ) : (
                      <>
                        <CareLevel value={careLevel} showScale={false} />
                        {!["easy", "medium", "demanding"].includes(careLevel) && "Не указана"}
                      </>
                    )}
                  </strong>
                </p>
              </div>
              <div>
                <span className="characteristic-icon pets">
                  <CharacteristicIcon name="pets" />
                </span>
                <p>
                  <small>Питомцы</small>
                  <strong>
                    {characteristic(
                      attributeLabel(
                        product.petSafety || product.passport.toxicity || "Не указано",
                      ),
                      "питом",
                      "безопас",
                      "токсич",
                      "pet",
                    )}
                  </strong>
                </p>
              </div>
              <div>
                <span className="characteristic-icon humidity">
                  <CharacteristicIcon name="humidity" />
                </span>
                <p>
                  <small>Влажность воздуха</small>
                  <strong>
                    {characteristic(
                      attributeLabel(product.passport.humidity || "Не указано"),
                      "влажн",
                      "humidity",
                    )}
                  </strong>
                </p>
              </div>
              {extraSummary.map((item) => (
                <div key={item.code}>
                  <span className="characteristic-icon care">
                    <CharacteristicIcon name="care" />
                  </span>
                  <p>
                    <small>{item.name}</small>
                    <strong>{displayAttributeValue(item)}</strong>
                  </p>
                </div>
              ))}
            </div>
          ) : (
            <div
              className="pdp-key-characteristics product-characteristics"
              aria-label="Основные характеристики товара"
            >
              <h2>Основные характеристики</h2>
              {allAttributes
                .filter((item) => item.showInCharacteristics !== false)
                .slice(0, 6)
                .map((item) => (
                  <div key={item.code}>
                    <p>
                      <small>{item.name}</small>
                      <strong>{displayAttributeValue(item)}</strong>
                    </p>
                  </div>
                ))}
            </div>
          )}
        </div>
      </div>
      {relatedPlants}
    </aside>
  );
}
