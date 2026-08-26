import type { Category, CategoryAttribute, Product } from "./adminTypes";

export type ReadinessCode = "no_photo" | "no_short_description" | "invalid_category" | "stage1_missing" | "latin_name_missing" | "care_missing" | "unknown_enum" | "draft" | "ready" | "published";
export type ProductReadiness = { codes: ReadinessCode[]; blockers: ReadinessCode[]; missingStage1: string[]; unknownEnums: string[] };

export const readinessLabels: Record<ReadinessCode, string> = {
  no_photo: "Нет основного фото", no_short_description: "Нет короткого описания",
  invalid_category: "Некорректная категория", stage1_missing: "Не заполнены поля этапа 1",
  latin_name_missing: "Нет латинского названия", care_missing: "Не заполнен уход",
  unknown_enum: "Есть неизвестные значения", draft: "Черновик",
  ready: "Готово к публикации", published: "Опубликовано",
};

export function rootCategory(categories: Category[], categoryId?: number) {
  let category = categories.find((item) => item.id === categoryId);
  const visited = new Set<number>();
  while (category?.parentId && !visited.has(category.id)) {
    visited.add(category.id);
    category = categories.find((item) => item.id === category?.parentId);
  }
  return category;
}

export function isPlantProduct(product: Pick<Product, "categoryId" | "catalogSection">, categories: Category[]) {
  const root = rootCategory(categories, product.categoryId);
  return root ? root.slug === "plants" : product.catalogSection === "plants";
}

export function valueMissing(value: unknown) {
  return value == null || value === "" || (Array.isArray(value) && value.length === 0);
}

export function readinessFor(product: Product, categories: Category[], schema: CategoryAttribute[]): ProductReadiness {
  const codes: ReadinessCode[] = [];
  const missingStage1 = schema.filter((item) => item.scope === "product" && item.keyCharacteristic && valueMissing(product.attributes?.[item.code])).map((item) => item.name);
  const unknownEnums = schema.filter((item) => item.scope === "product" && (item.dataType === "enum" || item.dataType === "multi_enum")).filter((item) => {
    const raw = product.attributes?.[item.code];
    const values = Array.isArray(raw) ? raw.map(String) : valueMissing(raw) ? [] : [String(raw)];
    return values.some((value) => !item.options.includes(value));
  }).map((item) => item.name);
  if (!product.image) codes.push("no_photo");
  if (!product.shortDescription.trim()) codes.push("no_short_description");
  const root = rootCategory(categories, product.categoryId);
  if (!product.categoryId || !categories.some((item) => item.id === product.categoryId) || !root || !["plants","pots","soil","fertilizer","accessories"].includes(root.slug)) codes.push("invalid_category");
  if (missingStage1.length || product.variantStage1Missing) codes.push("stage1_missing");
  if (isPlantProduct(product, categories) && !product.latinName.trim()) codes.push("latin_name_missing");
  if (isPlantProduct(product, categories) && (!product.careInstructions.trim() || !product.passport?.lighting || !product.passport?.watering)) codes.push("care_missing");
  if (unknownEnums.length || product.unknownEnumValues) codes.push("unknown_enum");
  const blockers = [...codes];
  if (product.status === "published") codes.push("published");
  else if (product.status === "draft") codes.push("draft");
  if (product.status !== "published" && blockers.length === 0) codes.push("ready");
  return { codes, blockers, missingStage1, unknownEnums };
}

export type AICandidate = { path: string; label: string; current: unknown; proposed: unknown; conflict: boolean };

export function readPath(source: unknown, path: string): unknown {
  return path.split(".").reduce<unknown>((value, key) => value && typeof value === "object" ? (value as Record<string, unknown>)[key] : undefined, source);
}

export function writePath<T>(source: T, path: string, value: unknown): T {
  const copy = structuredClone(source);
  const keys = path.split(".");
  let target = copy as Record<string, unknown>;
  keys.slice(0, -1).forEach((key) => {
    if (!target[key] || typeof target[key] !== "object") target[key] = {};
    target = target[key] as Record<string, unknown>;
  });
  target[keys[keys.length - 1]] = value;
  return copy;
}

export function aiCandidates(form: Product, proposal: Record<string, unknown>, schema: CategoryAttribute[], field?: string): AICandidate[] {
  const labels: Record<string, string> = { name: "Название", latinName: "Латинское название", shortDescription: "Короткое описание", description: "Описание", careInstructions: "Введение в уход", image: "Основное фото", warnings: "Предупреждения", faq: "FAQ" };
  const candidates: Array<{ path: string; label: string; value: unknown }> = [];
  ["name", "latinName", "shortDescription", "description", "careInstructions", "image"].forEach((key) => {
    if ((!field || field === key) && !valueMissing(proposal[key])) candidates.push({ path: key, label: labels[key], value: proposal[key] });
  });
  const attributes = proposal.attributes && typeof proposal.attributes === "object" ? proposal.attributes as Record<string, unknown> : {};
  schema.filter((item) => item.scope === "product").forEach((item) => {
    if ((!field || field === item.code) && !valueMissing(attributes[item.code])) candidates.push({ path: `attributes.${item.code}`, label: item.name, value: attributes[item.code] });
  });
  const passport = proposal.passport && typeof proposal.passport === "object" ? proposal.passport as Record<string, unknown> : {};
  Object.entries(passport).forEach(([key, value]) => {
    if ((!field || field === key || field === "faq" && key === "faq") && !valueMissing(value)) candidates.push({ path: `passport.${key}`, label: labels[key] || key, value });
  });
  if ((!field || field === "warnings") && !valueMissing(proposal.warnings)) candidates.push({ path: "importantWarnings", label: labels.warnings, value: proposal.warnings });
  return candidates.map((candidate) => {
    const current = readPath(form, candidate.path);
    return { ...candidate, current, proposed: candidate.value, conflict: !valueMissing(current) && JSON.stringify(current) !== JSON.stringify(candidate.value) };
  });
}

export function applyAICandidates(form: Product, candidates: AICandidate[], selected: Set<string>) {
  return candidates.reduce((result, candidate) => selected.has(candidate.path) ? writePath(result, candidate.path, candidate.proposed) : result, form);
}

export function displayValue(value: unknown, attribute?: CategoryAttribute) {
  if (valueMissing(value)) return "Не заполнено";
  const values = Array.isArray(value) ? value.map(String) : [String(value)];
  return values.map((item) => attribute?.optionLabels?.[item] || item).join(", ");
}
