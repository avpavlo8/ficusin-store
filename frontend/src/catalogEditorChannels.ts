import type { DraftVariant, ProductCreationDraft } from "./productCreationDraft";
export type ChannelCategory = { id: string; name: string; typeId?: number };
export type MarketplaceAttributeValue =
  | string
  | Array<{ value: string; dictionaryValueId?: number }>;
export type MarketplaceChannel = {
  article?: string;
  linkedArticle?: string;
  replaceLink?: boolean;
  enabled: boolean;
  mode: "create" | "link";
  existingId: string;
  category: ChannelCategory;
  attributes?: Record<string, MarketplaceAttributeValue>;
};
export type MarketplaceCommon = {
  contentFromMarketplace?: boolean;
  name: string;
  article: string;
  description: string;
  brand: string;
  brandId?: number;
  model: string;
  tnved: string;
  tnvedId?: number;
  units: number;
  ozon: MarketplaceChannel;
  wb: MarketplaceChannel;
};
export type SabyEditorConfig = {
  category: ChannelCategory;
  folder: ChannelCategory;
  mode: "create" | "link";
  existingCode: string;
};
export type SabyEditorField = keyof SabyEditorConfig;
export type SabyEditorTouched = Partial<Record<SabyEditorField, true>>;
export type SabyReadLink = {
  id: string;
  code: string;
  category: ChannelCategory;
  folder: ChannelCategory;
  mirrorFound: boolean;
};
export const blankCategory = (): ChannelCategory => ({ id: "", name: "" });
export const blankMarketplaceChannel = (): MarketplaceChannel => ({
  enabled: false,
  mode: "create",
  existingId: "",
  category: blankCategory(),
});
export function marketplaceOf(v: DraftVariant, d: ProductCreationDraft): MarketplaceCommon {
  const o = v.ozon || d.ozon,
    w = v.wb || d.wb;
  return (
    v.marketplace || {
      name: o.name || w.name,
      article: o.article || w.article || v.sku,
      description: o.description || w.description,
      brand: o.brand || w.brand || "",
      model: o.model || w.model || "",
      tnved: o.tnved || w.tnved || "",
      units: 1,
      ozon: { ...blankMarketplaceChannel(), enabled: o.enabled },
      wb: { ...blankMarketplaceChannel(), enabled: w.enabled },
    }
  );
}
export function sabyOf(v: DraftVariant, d: ProductCreationDraft): SabyEditorConfig {
  const linked = Boolean(
    v.sabyId ||
      v.externalIds?.some(
        (item) => item.provider === "saby" && item.type === "id" && item.externalId,
      ),
  );
  return (
    v.sabyEditor || {
      category: (!linked && d.sabyCategory) || blankCategory(),
      folder: (!linked && d.sabyFolder) || blankCategory(),
      mode: linked ? "link" : "create",
      existingCode: "",
    }
  );
}
export function marketplaceMissing(
  m: MarketplaceCommon,
  linked: Partial<Record<"wb" | "ozon", string>> = {},
): string[] {
  const missing: string[] = [];
  for (const [label, ch] of [
    ["Ozon", m.ozon],
    ["Wildberries", m.wb],
  ] as const) {
    if (linked[label === "Ozon" ? "ozon" : "wb"]) {
      if (ch.article !== undefined && !ch.article.trim()) missing.push(`Артикул продавца ${label}`);
      continue;
    }
    if (!ch.enabled && ch.mode !== "link") continue;
    if (ch.mode === "link") {
      if (!ch.existingId.trim()) missing.push(`Артикул существующего товара ${label}`);
      continue;
    }
    if (!ch.category.id) missing.push(`Категория ${label}`);
    for (const [value, title] of [
      [ch.article ?? m.article, `Артикул продавца ${label}`],
      [m.brand, "Бренд"],
      [m.tnved, "ТН ВЭД"],
    ] as const)
      if (!value.trim()) missing.push(title);
    if (label === "Ozon") {
      if (!m.model.trim()) missing.push("Название модели");
      if (m.units < 1) missing.push("Единиц в одном товаре");
    }
  }
  return [...new Set(missing)];
}
export type OzonAttribute = {
  id: number;
  name: string;
  is_required?: boolean;
  dictionary_id?: number;
  required?: boolean;
  charcID?: number;
  type?: string;
  charcType?: number;
  maxCount?: number;
  max_value_count?: number;
};
export function flattenCategories(channel: "ozon" | "wb", data: unknown): ChannelCategory[] {
  const raw = data as {
    result?: Array<Record<string, unknown>>;
    data?: Array<Record<string, unknown>>;
  };
  const result: ChannelCategory[] = [];
  const walk = (nodes: Array<Record<string, unknown>>, path: string[], parent: number) => {
    for (const n of nodes) {
      const title = String(n.category_name || n.type_name || n.subjectName || "");
      const category = Number(n.description_category_id || parent);
      const typeId = Number(n.type_id || 0);
      if (channel === "wb")
        result.push({
          id: String(n.subjectID),
          name: [n.parentName, title].filter(Boolean).join(" / "),
        });
      else if (typeId)
        result.push({
          id: String(category),
          typeId,
          name: [...path, title].filter(Boolean).join(" / "),
        });
      if (Array.isArray(n.children))
        walk(
          n.children as Array<Record<string, unknown>>,
          [...path, title].filter(Boolean),
          category,
        );
    }
  };
  walk(raw?.result || raw?.data || [], [], 0);
  return result;
}
