import { blankCategory, blankMarketplaceChannel } from "./catalogEditorChannels.ts";
import type { MarketplaceCommon, SabyEditorConfig } from "./catalogEditorChannels.ts";
import type { DraftVariant, ProductCreationDraft } from "./productCreationDraft";
import { normalizePlantAttributes } from "./plantCharacteristicSchema.ts";

type Fields = Record<string, unknown>;
const object = (value: unknown): Fields =>
  value != null && typeof value === "object" && !Array.isArray(value) ? (value as Fields) : {};
const own = (value: Fields, key: string) => Object.prototype.hasOwnProperty.call(value, key);
const text = (value: unknown, fallback = "") => (typeof value === "string" ? value : fallback);

// The only importer of the retired top-level/legacy per-channel draft fields.
// New runtime state has one complete marketplace/Saby configuration per SKU.
export function normalizeDraftVariant(value: Fields, root: Fields = {}): DraftVariant {
  const ozone = object(value.ozon ?? root.ozon),
    wb = object(value.wb ?? root.wb);
  const previous = object(value.marketplace);
  const article = text(previous.article, text(ozone.article || wb.article, text(value.sku)));
  const channel = (name: "ozon" | "wb", old: Fields) => {
    const saved = object(previous[name]);
    return {
      ...blankMarketplaceChannel(),
      ...saved,
      enabled: typeof saved.enabled === "boolean" ? saved.enabled : old.enabled === true,
      category: {
        ...blankCategory(),
        ...(typeof old.category === "string" ? { name: old.category } : object(old.category)),
        ...object(saved.category),
      },
      article:
        typeof saved.article === "string"
          ? saved.article
          : own(value, "marketplace")
            ? article
            : typeof old.article === "string"
              ? old.article
              : article,
    };
  };
  const marketplace: MarketplaceCommon = {
    name: text(ozone.name || wb.name),
    article,
    description: text(ozone.description || wb.description),
    brand: text(ozone.brand || wb.brand),
    model: text(ozone.model || wb.model),
    tnved: text(ozone.tnved || wb.tnved),
    units: 1,
    ...previous,
    ozon: channel("ozon", ozone),
    wb: channel("wb", wb),
  };
  const externalIds = Array.isArray(value.externalIds) ? value.externalIds : [];
  const linked = Boolean(
    value.sabyId ||
      externalIds.some((id: Fields) => id.provider === "saby" && id.type === "id" && id.externalId),
  );
  const oldSaby = object(value.saby ?? root.saby),
    selected = object(oldSaby.selected);
  const config = object(value.sabyEditor);
  const sabyEditor: SabyEditorConfig = {
    mode: linked ? "link" : oldSaby.mode === "link" ? "link" : "create",
    existingCode: text(selected.code),
    ...config,
    category: {
      ...blankCategory(),
      ...object(!linked ? (object(root.sabyDefaults).category ?? root.sabyCategory) : undefined),
      ...object(config.category),
    },
    folder: {
      ...blankCategory(),
      ...(oldSaby.folderId ? { id: text(oldSaby.folderId) } : {}),
      ...object(!linked ? (object(root.sabyDefaults).folder ?? root.sabyFolder) : undefined),
      ...object(config.folder),
    },
  };
  const attributes = draftAttributes(value.attributes);
  for (const [code, legacy] of [
    ["pot_diameter_cm", root.pot],
    ["height_cm", root.height],
  ] as const)
    if (
      !own(attributes, code) &&
      typeof legacy === "string" &&
      legacy.trim() &&
      Number.isFinite(Number(legacy))
    )
      attributes[code] = Number(legacy);
  const result: Fields = {
    ...value,
    id: text(value.id, "first"),
    label: text(value.label, "Основной вариант"),
    sku: text(value.sku),
    price: text(value.price, text(root.price)),
    wholesaleMinQty: text(value.wholesaleMinQty, "1"),
    active: value.active ?? true,
    archived: value.archived ?? false,
    attributes: normalizePlantAttributes(attributes, !root.plantAttributesVersion),
    externalIds,
    marketplace,
    marketplacePriceMode:
      value.marketplacePriceMode ??
      (root.version === 1 &&
      [ozone.price, wb.price].some((p) => typeof p === "string" && p.trim() !== "")
        ? "manual"
        : undefined),
    marketplaceInitialized:
      value.marketplaceInitialized ??
      (own(value, "marketplace") ||
        [ozone, wb].some(
          (c) =>
            ["name", "article", "description", "brand", "model", "tnved"].some((key) =>
              Boolean(c[key]),
            ) || c.enabled,
        )),
    sabyEditor,
    ozon: { price: text(ozone.price) },
    wb: { price: text(wb.price) },
  };
  delete result.saby;
  return result as DraftVariant;
}

export function normalizeProductDraft(
  value: Fields,
  fallback: ProductCreationDraft,
): ProductCreationDraft {
  if (value.version !== 1 && value.version !== 2) throw new Error("Unknown draft version");
  if (typeof value.name !== "string" || !Array.isArray(value.media) || !Array.isArray(value.faq))
    throw new Error("Invalid draft");
  if (value.variants != null && !Array.isArray(value.variants)) throw new Error("Invalid variants");
  for (const media of [
    value.media,
    ...(Array.isArray(value.variants) ? value.variants : []).map((v: Fields) => v.media || []),
  ])
    if (
      !Array.isArray(media) ||
      media.some(
        (photo: Fields) =>
          !photo ||
          typeof photo.id !== "string" ||
          typeof photo.name !== "string" ||
          typeof photo.type !== "string",
      )
    )
      throw new Error("Invalid media draft");
  const next = { ...fallback, ...value };
  const rows =
    Array.isArray(value.variants) && value.variants.length
      ? value.variants
      : [{ id: "first", attributes: object(value.attributes) }];
  for (const row of rows) {
    if (!row || typeof row !== "object" || Array.isArray(row))
      throw new Error("Invalid variant draft");
    for (const key of ["id", "sku", "label", "price", "wholesaleMinQty"])
      if (own(row, key) && typeof row[key] !== "string") throw new Error("Invalid variant field");
    if (own(row, "serverId") && (!Number.isSafeInteger(row.serverId) || row.serverId <= 0))
      throw new Error("Invalid server variant identity");
  }
  const variants = rows.map((v: Fields) => normalizeDraftVariant(v, value));
  const localIds = new Set<string>(),
    serverIds = new Set<number>();
  for (const variant of variants) {
    if (!variant.id.trim() || localIds.has(variant.id))
      throw new Error("Ambiguous draft variant identity");
    localIds.add(variant.id);
    if (variant.serverId != null) {
      if (serverIds.has(variant.serverId)) throw new Error("Duplicate server variant identity");
      serverIds.add(variant.serverId);
    }
  }
  const result: Fields = {
    ...next,
    version: 2,
    variants,
    remoteInitialized:
      value.remoteInitialized ?? variants.some((v: DraftVariant) => Boolean(v.serverId)),
    sabyDefaults: {
      category: object(rootDefault(value, "category")),
      folder: object(rootDefault(value, "folder")),
    },
    selectedVariantId: variants.some((v: DraftVariant) => v.id === value.selectedVariantId)
      ? value.selectedVariantId
      : variants[0].id,
    attributes: normalizePlantAttributes(
      draftAttributes(next.attributes),
      !value.plantAttributesVersion,
    ),
    plantAttributesVersion: 1,
    editorTab:
      value.editorTab ||
      (value.channelTab || ["saby", "ozon", "wb"].includes(text(value.step))
        ? "channels"
        : value.step === "care"
          ? "faq"
          : "product"),
    ozon: { price: text(object(next.ozon).price) },
    wb: { price: text(object(next.wb).price) },
  };
  for (const key of ["saby", "sabyCategory", "sabyFolder", "step", "channelTab", "pot", "height"])
    delete result[key];
  return result as ProductCreationDraft;
}

function rootDefault(value: Fields, field: "category" | "folder") {
  return (
    object(value.sabyDefaults)[field] ??
    value[field === "category" ? "sabyCategory" : "sabyFolder"] ??
    blankCategory()
  );
}

function draftAttributes(value: unknown): DraftVariant["attributes"] {
  const fields = { ...object(value) };
  if (
    Object.values(fields).some(
      (v) =>
        v !== null &&
        !["string", "number", "boolean"].includes(typeof v) &&
        !(Array.isArray(v) && v.every((x) => typeof x === "string")),
    )
  )
    throw new Error("Invalid draft attributes");
  return fields as DraftVariant["attributes"];
}
