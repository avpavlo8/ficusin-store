// Product data and seller transports use grams. Editors display kilograms;
// conversion happens only at their input boundary, never while loading drafts.
export const packagingWeightInput = { min: "0.001", step: "0.001" } as const;

export function gramsToKilograms(value: unknown): string {
  if (value == null || value === "") return "";
  if (typeof value !== "number" && typeof value !== "string") return "";
  if (typeof value === "string" && !value.trim()) return "";
  const grams = Number(value);
  return Number.isFinite(grams) && grams >= 0 ? String(grams / 1000) : "";
}

export function kilogramsToGrams(value: string): number | null {
  const text = value.trim().replace(",", ".");
  if (!/^(?:\d+(?:\.\d*)?|\.\d+)$/.test(text)) return null;
  const grams = Math.round(Number(text) * 1000);
  return Number.isSafeInteger(grams) ? grams : null;
}
