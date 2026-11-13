import type { DraftVariant } from "./productCreationDraft";

// Category rules describe which variant attribute supplies its public name.
const namingAttributes: Record<string, { code: string; prefix: string }> = {
  plants: { code: "pot_diameter_cm", prefix: "D" },
};

export function categoryVariantName(variant: DraftVariant, categorySection?: string): string {
  const rule = categorySection ? namingAttributes[categorySection] : undefined;
  if (!rule) return variant.label;
  const value = variant.attributes[rule.code];
  if (typeof value !== "number" && typeof value !== "string") return "";
  if (typeof value === "string" && !value.trim()) return "";
  const number = Number(typeof value === "string" ? value.replace(",", ".") : value);
  return Number.isFinite(number) && number > 0 ? `${rule.prefix}${number}` : "";
}
