import type { Product, CategoryAttribute } from "./adminTypes";
import type { AdminVariant } from "./AdminPim";
import {
  emptyProductDraft,
  type ProductCreationDraft,
  type DraftVariant,
} from "./productCreationDraft";

export function productToDraft(product: Product): ProductCreationDraft {
  return {
    ...emptyProductDraft(),
    name: product.name,
    description: product.description,
    latinName: product.latinName || "",
    categoryId: product.categoryId,
    price: String(product.price),
    pot: product.potDiameterCm == null ? "" : String(product.potDiameterCm),
    height: product.heightCm == null ? "" : String(product.heightCm),
    attributes: { ...product.attributes },
    care: product.careInstructions || "",
    passport: { ...product.passport },
    faq: (product.passport?.faq || []).map((item, i) => ({ ...item, id: `faq-${i}` })),
    warnings: (product.importantWarnings || []).join("\n"),
    media: product.image
      ? [
          {
            id: `cover-${product.id}`,
            source: product.image,
            name: "Основное фото",
            type: "image/jpeg",
          },
        ]
      : [],
  };
}
export function variantToDraft(variant: AdminVariant): DraftVariant {
  return {
    id: `variant-${variant.id}`,
    serverId: variant.id,
    label: variant.label,
    sku: variant.sku,
    price: String(variant.price),
    wholesaleMinQty: String(variant.wholesaleMinQty),
    active: variant.active,
    archived: variant.archived,
    attributes: { ...variant.attributes },
    externalIds: variant.externalIds.map((item) => ({
      provider: item.provider,
      type: item.type,
      externalId: item.externalId,
    })),
    sabyId: variant.externalIds.find((item) => item.provider === "saby" && item.type === "id")
      ?.externalId,
  };
}
export function seedVariant(draft: ProductCreationDraft): DraftVariant {
  return {
    id: "first",
    label: "Основной вариант",
    sku: "",
    price: draft.price,
    wholesaleMinQty: "1",
    active: true,
    archived: false,
    externalIds: [],
    ozon: { ...draft.ozon },
    wb: { ...draft.wb },
    attributes: {
      ...draft.attributes,
      pot_diameter_cm: draft.attributes.pot_diameter_cm ?? (draft.pot ? Number(draft.pot) : null),
      height_cm: draft.attributes.height_cm ?? (draft.height ? Number(draft.height) : null),
    },
  };
}
export function attributeDelta(
  schema: CategoryAttribute[],
  values: Record<string, unknown>,
  original: Record<string, unknown>,
): Record<string, unknown> {
  const changes: Record<string, unknown> = {};
  for (const item of schema) {
    const next = values[item.code];
    if (JSON.stringify(next) === JSON.stringify(original[item.code])) continue;
    changes[item.code] =
      next == null || next === "" || (Array.isArray(next) && !next.length) ? null : next;
  }
  return changes;
}
