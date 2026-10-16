import type { Product } from "./adminTypes";

// Only editorial/product metadata may be reconciled. SKU identities, inventory,
// prices, publication, media, categories and channel mappings never enter this patch.
export const mergeContentFields = [
  ["shortDescription", "Прежнее краткое описание"], ["description", "Описание"],
  ["careInstructions", "Введение в уход"], ["passport", "Паспорт и вопросы"],
  ["importantWarnings", "Предупреждения"], ["plantKind", "Тип растения"],
  ["lightLevel", "Освещение"], ["watering", "Полив"], ["heightClass", "Группа высоты"],
  ["careLevel", "Сложность ухода"], ["placement", "Размещение"],
  ["petSafety", "Безопасность для животных"], ["growthHabit", "Форма роста"],
] as const;

function meaningful(value: unknown): boolean {
  if (Array.isArray(value)) return value.length > 0;
  if (value && typeof value === "object") return Object.values(value).some(meaningful);
  return value != null && value !== "";
}

function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object") return `{${Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, item]) => `${JSON.stringify(key)}:${canonical(item)}`).join(",")}}`;
  return JSON.stringify(value) ?? "null";
}

export function mergeContentPatch(target: Product, source: Product): Record<string, unknown> {
  return Object.fromEntries(mergeContentFields.filter(([key]) => meaningful(target[key]) && meaningful(source[key]) && canonical(target[key]) !== canonical(source[key])).map(([key]) => [key, target[key]]));
}

export function canReconcileMergeContent(products: Product[], target: Product | undefined): target is Product {
  return !!target && !!target.categoryId && products.length > 1 && products.every(item => item.status === "published" && item.catalogSection === "plants" && item.categoryId === target.categoryId && !!item.latinName.trim() && item.latinName.trim().toLowerCase() === target.latinName.trim().toLowerCase() && item.name.trim().toLowerCase() === target.name.trim().toLowerCase());
}
